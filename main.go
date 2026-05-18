// journal-mcp is a tiny MCP server that appends bullet entries to a named
// section of today's Obsidian daily-note. It exposes a single tool,
// write_journal_entry, and listens on loopback only.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	addr          = flag.String("addr", "127.0.0.1:17310", "loopback address to listen on (Streamable HTTP)")
	dockerNetwork = flag.String("docker-network", "", "if set, bind to the host-side gateway IP of this docker bridge network instead of -addr's host; the listener is then reachable from containers attached to that network but not from the LAN. Reuses the port from -addr.")
	dockerSocket  = flag.String("docker-socket", "/var/run/docker.sock", "path to the docker engine socket; used only with -docker-network")
	vaultDir      = flag.String("vault-daily-dir", "", "directory containing daily notes; default $HOME/Documents/vault/Journal/Daily")
	section       = flag.String("section", "Overmind Notes", "H2 section heading to append entries under")
	noTimestamp   = flag.Bool("no-timestamp", false, "do not prefix each bullet with the current HH:MM")
)

// writeMu serialises edits to the daily note so concurrent tool calls cannot
// trample each other's writes. The journal is tiny; a process-wide mutex is fine.
var writeMu sync.Mutex

// WriteJournalEntryArgs is the input schema for the write_journal_entry tool.
type WriteJournalEntryArgs struct {
	Entries []string `json:"entries" jsonschema:"one or more bullet entries to append; each becomes its own list item"`
}

// WriteJournalEntryResult tells the agent exactly what was written and where.
type WriteJournalEntryResult struct {
	File          string   `json:"file"`
	Section       string   `json:"section"`
	WrittenBullets []string `json:"written_bullets"`
}

func main() {
	flag.Parse()

	if *vaultDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatalf("resolve home dir: %v", err)
		}
		*vaultDir = filepath.Join(home, "Documents", "vault", "Journal", "Daily")
	}
	if err := os.MkdirAll(*vaultDir, 0o755); err != nil {
		log.Fatalf("ensure vault dir %s: %v", *vaultDir, err)
	}

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "journal-mcp",
		Version: "0.1.0",
	}, nil)

	mcp.AddTool(server, &mcp.Tool{
		Name:        "write_journal_entry",
		Description: "Append one or more bullet items to the '" + *section + "' section of today's Obsidian daily note. Creates the file and the section if either is missing. Use one bullet per discrete piece of work.",
	}, handleWriteJournalEntry)

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server {
		return server
	}, nil)

	// Resolve the bind address. Default path is loopback-only. The single
	// escape hatch is -docker-network, which binds to the host-side gateway
	// of a named docker bridge — reachable from containers on that bridge,
	// not from anything that isn't already inside that overlay.
	if *dockerNetwork != "" {
		resolved, err := resolveDockerNetworkAddr(*dockerSocket, *dockerNetwork, *addr)
		if err != nil {
			log.Fatalf("resolve -docker-network %q: %v", *dockerNetwork, err)
		}
		log.Printf("binding to gateway of docker network %q: %s", *dockerNetwork, resolved)
		*addr = resolved
	} else if err := assertLoopback(*addr); err != nil {
		log.Fatalf("refusing to listen on non-loopback address: %v", err)
	}

	log.Printf("journal-mcp listening on http://%s  vault=%s  section=%q", *addr, *vaultDir, *section)
	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("listen: %v", err)
	}
}

func handleWriteJournalEntry(ctx context.Context, _ *mcp.CallToolRequest, args *WriteJournalEntryArgs) (*mcp.CallToolResult, *WriteJournalEntryResult, error) {
	entries := make([]string, 0, len(args.Entries))
	for _, e := range args.Entries {
		s := strings.TrimSpace(e)
		if s != "" {
			entries = append(entries, s)
		}
	}
	if len(entries) == 0 {
		return nil, nil, fmt.Errorf("entries must contain at least one non-empty string")
	}

	now := time.Now()
	bullets := make([]string, len(entries))
	for i, e := range entries {
		bullets[i] = formatBullet(e, now, !*noTimestamp)
	}

	path := filepath.Join(*vaultDir, now.Format("2006-01-02")+".md")

	writeMu.Lock()
	defer writeMu.Unlock()
	if err := appendBulletsToSection(path, *section, bullets, now); err != nil {
		return nil, nil, fmt.Errorf("update %s: %w", path, err)
	}

	res := &WriteJournalEntryResult{
		File:           path,
		Section:        *section,
		WrittenBullets: bullets,
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Appended %d bullet(s) to %q in %s", len(bullets), *section, path),
		}},
	}, res, nil
}

func formatBullet(entry string, now time.Time, prefixTime bool) string {
	entry = strings.TrimSpace(entry)
	if prefixTime {
		return fmt.Sprintf("- %s — %s", now.Format("15:04"), entry)
	}
	return "- " + entry
}

// appendBulletsToSection appends bullets under the H2 `## <section>` heading
// in the markdown file at path, creating the file and/or section if needed.
// Writes atomically via a temp file in the same directory.
func appendBulletsToSection(path, section string, bullets []string, now time.Time) error {
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	updated := updateMarkdown(string(existing), section, bullets, now)

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".journal-mcp-*.md.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpPath) }
	if _, err := tmp.WriteString(updated); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		cleanup()
		return err
	}
	return os.Rename(tmpPath, path)
}

// h2Re matches the start of an H2 heading line (`## Title`). We intentionally
// do NOT match H1 (`# `) as a section boundary because an H1 typically opens
// the daily note and we want all H2 sections to live beneath it.
var (
	h2Re      = regexp.MustCompile(`^## +(.+?)\s*$`)
	anyHeadRe = regexp.MustCompile(`^#{1,6} `)
)

// updateMarkdown is the pure string transform; main.go logic and tests share it.
func updateMarkdown(content, section string, bullets []string, now time.Time) string {
	wantHeading := "## " + section

	if strings.TrimSpace(content) == "" {
		// New file: H1 with today's date, then the section, then bullets.
		var b strings.Builder
		fmt.Fprintf(&b, "# %s\n\n%s\n\n", now.Format("2006-01-02"), wantHeading)
		for _, bl := range bullets {
			b.WriteString(bl)
			b.WriteByte('\n')
		}
		return b.String()
	}

	// Normalise: split on \n but remember whether the file ended in \n so we
	// can restore it. strings.Split("a\n", "\n") returns ["a", ""], which
	// otherwise leaves an unwanted trailing empty line in the rebuild.
	trailingNL := strings.HasSuffix(content, "\n")
	body := content
	if trailingNL {
		body = strings.TrimSuffix(body, "\n")
	}
	lines := strings.Split(body, "\n")

	// Find the section heading.
	sectionIdx := -1
	for i, l := range lines {
		m := h2Re.FindStringSubmatch(l)
		if m != nil && strings.EqualFold(strings.TrimSpace(m[1]), section) {
			sectionIdx = i
			break
		}
	}

	if sectionIdx == -1 {
		// Section missing: append it at end of file with a blank-line buffer.
		out := strings.TrimRight(content, "\n")
		var b strings.Builder
		b.WriteString(out)
		b.WriteString("\n\n")
		b.WriteString(wantHeading)
		b.WriteString("\n\n")
		for _, bl := range bullets {
			b.WriteString(bl)
			b.WriteByte('\n')
		}
		return b.String()
	}

	// Find the end of the section: the next heading line, or EOF.
	end := len(lines)
	for i := sectionIdx + 1; i < len(lines); i++ {
		if anyHeadRe.MatchString(lines[i]) {
			end = i
			break
		}
	}

	// Trim trailing blank lines that belong to the existing section body so
	// the new bullets sit flush against the previous ones.
	insertAt := end
	for insertAt > sectionIdx+1 && strings.TrimSpace(lines[insertAt-1]) == "" {
		insertAt--
	}

	// Build the new section body: everything up to insertAt, then bullets,
	// then a single blank line (only if there's a following heading), then
	// the rest.
	newLines := make([]string, 0, len(lines)+len(bullets)+2)
	newLines = append(newLines, lines[:insertAt]...)
	newLines = append(newLines, bullets...)
	if end < len(lines) {
		newLines = append(newLines, "")
	}
	newLines = append(newLines, lines[end:]...)

	out := strings.Join(newLines, "\n")
	if trailingNL || end == len(lines) {
		// Either the file originally ended in \n, or we just appended bullets
		// at the very end. In both cases POSIX wants a trailing newline.
		out += "\n"
	}
	return out
}

// resolveDockerNetworkAddr returns "<gateway-ip>:<port>" where the IP is the
// host-side gateway of the named docker bridge network and the port is the
// port from fallbackAddr. It refuses to return a non-private, non-loopback
// address so a misconfigured docker network can't trick the server into
// binding to a publicly-reachable IP.
func resolveDockerNetworkAddr(socketPath, network, fallbackAddr string) (string, error) {
	_, port, err := net.SplitHostPort(fallbackAddr)
	if err != nil {
		return "", fmt.Errorf("parse port from -addr %q: %w", fallbackAddr, err)
	}
	ip, err := dockerNetworkGateway(socketPath, network)
	if err != nil {
		return "", err
	}
	if !ip.IsPrivate() && !ip.IsLoopback() {
		return "", fmt.Errorf("gateway %s is neither RFC1918 nor loopback; refusing to bind", ip)
	}
	return net.JoinHostPort(ip.String(), port), nil
}

// dockerNetworkGateway asks the docker daemon (over its unix socket) for the
// IPAM gateway of the named network. No dependency on the `docker` CLI.
func dockerNetworkGateway(socketPath, network string) (net.IP, error) {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
			},
		},
	}
	// The host portion of the URL is ignored by the unix-socket dialer above;
	// docker just wants the path.
	req, err := http.NewRequest(http.MethodGet, "http://docker/networks/"+url.PathEscape(network), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("query docker daemon at %s: %w", socketPath, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker GET /networks/%s: %s: %s", network, resp.Status, strings.TrimSpace(string(body)))
	}
	return parseDockerNetworkGateway(body)
}

// parseDockerNetworkGateway pulls the first non-empty IPAM gateway out of a
// docker network-inspect response body. Split out for testability.
func parseDockerNetworkGateway(body []byte) (net.IP, error) {
	var doc struct {
		IPAM struct {
			Config []struct {
				Gateway string `json:"Gateway"`
			} `json:"Config"`
		} `json:"IPAM"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse docker network JSON: %w", err)
	}
	for _, c := range doc.IPAM.Config {
		if c.Gateway == "" {
			continue
		}
		ip := net.ParseIP(c.Gateway)
		if ip == nil {
			return nil, fmt.Errorf("docker returned malformed gateway %q", c.Gateway)
		}
		return ip, nil
	}
	return nil, errors.New("docker network has no IPAM gateway configured")
}

// assertLoopback ensures the listen address resolves to 127.0.0.0/8 or ::1.
func assertLoopback(a string) error {
	host, _, err := net.SplitHostPort(a)
	if err != nil {
		return err
	}
	if host == "" || host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("address host %q is not an IP literal", host)
	}
	if !ip.IsLoopback() {
		return fmt.Errorf("address host %q is not a loopback IP", host)
	}
	return nil
}
