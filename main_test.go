package main

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tt, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time: %v", err)
	}
	return tt
}

func TestUpdateMarkdown_EmptyFile(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	got := updateMarkdown("", "Overmind Notes", []string{"- 14:27 — first thing"}, now)
	want := "# 2026-05-18\n\n## Overmind Notes\n\n- 14:27 — first thing\n"
	if got != want {
		t.Errorf("empty file:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestUpdateMarkdown_SectionMissing(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "# 2026-05-18\n\nSome preamble.\n\n## Meetings\n- 10:00 standup\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- 14:27 — bullet"}, now)
	if !strings.Contains(got, "## Overmind Notes\n\n- 14:27 — bullet\n") {
		t.Errorf("expected section to be appended; got:\n%s", got)
	}
	if !strings.HasPrefix(got, in[:len("# 2026-05-18\n\nSome preamble.\n\n## Meetings\n- 10:00 standup")]) {
		t.Errorf("expected existing content preserved; got:\n%s", got)
	}
}

func TestUpdateMarkdown_SectionPresent_InsertBeforeNextHeading(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "# 2026-05-18\n\n## Overmind Notes\n\n- 09:00 — early\n- 11:00 — mid\n\n## Tomorrow\n- buy milk\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- 14:27 — new"}, now)
	want := "# 2026-05-18\n\n## Overmind Notes\n\n- 09:00 — early\n- 11:00 — mid\n- 14:27 — new\n\n## Tomorrow\n- buy milk\n"
	if got != want {
		t.Errorf("want:\n%q\ngot:\n%q", want, got)
	}
}

func TestUpdateMarkdown_SectionPresent_AtEndOfFile(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "# 2026-05-18\n\n## Overmind Notes\n\n- 09:00 — early\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- 14:27 — new", "- 14:28 — newer"}, now)
	want := "# 2026-05-18\n\n## Overmind Notes\n\n- 09:00 — early\n- 14:27 — new\n- 14:28 — newer\n"
	if got != want {
		t.Errorf("want:\n%q\ngot:\n%q", want, got)
	}
}

func TestUpdateMarkdown_SectionPresent_Empty(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "# 2026-05-18\n\n## Overmind Notes\n\n## Tomorrow\n- buy milk\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- 14:27 — first"}, now)
	want := "# 2026-05-18\n\n## Overmind Notes\n- 14:27 — first\n\n## Tomorrow\n- buy milk\n"
	if got != want {
		t.Errorf("want:\n%q\ngot:\n%q", want, got)
	}
}

func TestUpdateMarkdown_CaseInsensitiveMatch(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	in := "## overmind notes\n- existing\n"
	got := updateMarkdown(in, "Overmind Notes", []string{"- new"}, now)
	if !strings.Contains(got, "- existing\n- new") {
		t.Errorf("expected case-insensitive match; got:\n%s", got)
	}
}

func TestFormatBullet(t *testing.T) {
	now := mustTime(t, "2026-05-18T14:27:00+02:00")
	if got := formatBullet("  hello  ", now, true); got != "- 14:27 — hello" {
		t.Errorf("with timestamp: got %q", got)
	}
	if got := formatBullet("hello", now, false); got != "- hello" {
		t.Errorf("without timestamp: got %q", got)
	}
}

func TestAssertLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:17310": true,
		"localhost:17310": true,
		"[::1]:17310":     true,
		"0.0.0.0:17310":   false,
		"10.0.0.5:17310":  false,
		"example.com:80":  false,
	}
	for addr, wantOK := range cases {
		err := assertLoopback(addr)
		if (err == nil) != wantOK {
			t.Errorf("assertLoopback(%q): err=%v wantOK=%v", addr, err, wantOK)
		}
	}
}

func TestParseDockerNetworkGateway(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		body := []byte(`{"IPAM":{"Config":[{"Subnet":"172.20.0.0/16","Gateway":"172.20.0.1"}]}}`)
		ip, err := parseDockerNetworkGateway(body)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ip.String() != "172.20.0.1" {
			t.Errorf("got %s, want 172.20.0.1", ip)
		}
	})

	t.Run("skips empty gateway in first config", func(t *testing.T) {
		body := []byte(`{"IPAM":{"Config":[{"Subnet":"fd00::/64"},{"Gateway":"172.20.0.1"}]}}`)
		ip, err := parseDockerNetworkGateway(body)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ip.String() != "172.20.0.1" {
			t.Errorf("got %s, want 172.20.0.1", ip)
		}
	})

	t.Run("no gateway", func(t *testing.T) {
		body := []byte(`{"IPAM":{"Config":[]}}`)
		if _, err := parseDockerNetworkGateway(body); err == nil {
			t.Fatal("expected error for missing gateway")
		}
	})

	t.Run("malformed ip", func(t *testing.T) {
		body := []byte(`{"IPAM":{"Config":[{"Gateway":"nope"}]}}`)
		if _, err := parseDockerNetworkGateway(body); err == nil {
			t.Fatal("expected error for malformed ip")
		}
	})

	t.Run("bad json", func(t *testing.T) {
		if _, err := parseDockerNetworkGateway([]byte("{")); err == nil {
			t.Fatal("expected error for malformed json")
		}
	})
}

// fakeDockerDaemon spins up a tiny HTTP server on a unix socket inside
// t.TempDir() that mimics docker's GET /networks/<name>. Returns the socket
// path; the server is cleaned up automatically on test end.
func fakeDockerDaemon(t *testing.T, handler http.Handler) string {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen unix: %v", err)
	}
	srv := &httptest.Server{
		Listener: ln,
		Config:   &http.Server{Handler: handler, ReadHeaderTimeout: time.Second},
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return sockPath
}

func TestResolveDockerNetworkAddr(t *testing.T) {
	t.Run("private gateway", func(t *testing.T) {
		sock := fakeDockerDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/networks/overmind_default" {
				t.Errorf("unexpected path %s", r.URL.Path)
			}
			_, _ = w.Write([]byte(`{"IPAM":{"Config":[{"Gateway":"172.18.0.1"}]}}`))
		}))
		got, err := resolveDockerNetworkAddr(sock, "overmind_default", "127.0.0.1:17310")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if got != "172.18.0.1:17310" {
			t.Errorf("got %q, want 172.18.0.1:17310", got)
		}
	})

	t.Run("refuses public gateway", func(t *testing.T) {
		sock := fakeDockerDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"IPAM":{"Config":[{"Gateway":"8.8.8.8"}]}}`))
		}))
		if _, err := resolveDockerNetworkAddr(sock, "weird", "127.0.0.1:17310"); err == nil {
			t.Fatal("expected refusal for public gateway")
		}
	})

	t.Run("404 from docker", func(t *testing.T) {
		sock := fakeDockerDaemon(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"message":"network missing not found"}`, http.StatusNotFound)
		}))
		_, err := resolveDockerNetworkAddr(sock, "missing", "127.0.0.1:17310")
		if err == nil || !strings.Contains(err.Error(), "404") {
			t.Fatalf("expected 404 in error, got %v", err)
		}
	})

	t.Run("bad port in fallback addr", func(t *testing.T) {
		if _, err := resolveDockerNetworkAddr("/dev/null", "x", "no-port-here"); err == nil {
			t.Fatal("expected error parsing port")
		}
	})
}
