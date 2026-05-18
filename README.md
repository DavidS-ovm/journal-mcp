# journal-mcp

A minimal MCP server that exposes a single tool, `write_journal_entry`, which
appends bullet items to a named section (default `## Overmind Notes`) of today's
Obsidian daily note (default `~/Documents/vault/Journal/Daily/YYYY-MM-DD.md`).

- Localhost only (refuses to bind to anything outside `127.0.0.0/8` / `::1`).
- Streamable HTTP transport on `127.0.0.1:17310` (Cursor's MCP client does
  not yet support unix domain sockets; loopback TCP is the next-best option).
- Creates the daily file and the section if either is missing.
- Bullets are timestamped `- HH:MM — text` by default; pass `-no-timestamp`
  to disable.
- Concurrent writes are serialised in-process and committed atomically via
  `rename(2)` to keep Obsidian's file watcher happy.

## Install

```bash
# 1. Build and stage the binary.
cd ~/Projects/journal-mcp
go build -o ~/.local/bin/journal-mcp .

# 2. Drop the unit into the user systemd dir.
mkdir -p ~/.config/systemd/user
cp journal-mcp.service ~/.config/systemd/user/

# 3. Enable + start.
systemctl --user daemon-reload
systemctl --user enable --now journal-mcp.service

# 4. Sanity check.
systemctl --user status journal-mcp.service
journalctl --user -u journal-mcp.service -f
curl -i http://127.0.0.1:17310/   # MCP endpoint; should respond, not refuse.
```

If your user session does not survive logout (i.e. `loginctl show-user $USER`
shows `Linger=no`), enable lingering so the unit runs when you're not logged
in:

```bash
sudo loginctl enable-linger $USER
```

## Wire it into Cursor

Add to `~/.cursor/mcp.json` (global) or `<repo>/.cursor/mcp.json` (project):

```json
{
  "mcpServers": {
    "journal": {
      "url": "http://127.0.0.1:17310/"
    }
  }
}
```

Restart Cursor (or toggle the MCP server in Settings → MCP) and the
`write_journal_entry` tool will be available to the agent.

## Tool contract

```jsonc
// write_journal_entry
{
  "entries": [
    "Investigated MCP transport support in Cursor",
    "Wrote the journal-mcp service"
  ]
}
```

Result: each entry becomes a bullet under `## Overmind Notes` in today's daily
note, prefixed with `HH:MM` of when the tool was called.

## Flags

| Flag                | Default                                       | Meaning                                                     |
| ------------------- | --------------------------------------------- | ----------------------------------------------------------- |
| `-addr`             | `127.0.0.1:17310`                             | Loopback address to listen on. Non-loopback is refused.     |
| `-vault-daily-dir`  | `$HOME/Documents/vault/Journal/Daily`         | Directory containing daily notes. Auto-created if missing.  |
| `-section`          | `Overmind Notes`                              | H2 section heading to append under. Match is case-insensitive. |
| `-no-timestamp`     | `false`                                       | Disable the `HH:MM — ` prefix on each bullet.               |

## Limitations

- Single section per server instance. Run a second instance on a different
  port with `-section "Personal Notes"` if you want a second tool.
- No auth. Loopback only is the security boundary; anything that can connect
  to your loopback can write to the journal. This is fine for a personal
  workstation; do not deploy this anywhere else.
- The H2 heading match is case-insensitive but otherwise exact (no fuzzy
  matching, no whitespace normalisation beyond `TrimSpace`).
