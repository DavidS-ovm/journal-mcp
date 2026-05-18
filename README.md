# journal-mcp

A minimal MCP server that exposes a single tool, `write_journal_entry`, which
appends bullet items to a named section (default `## Overmind Notes`) of today's
Obsidian daily note (default `~/Documents/vault/Journal/Daily/YYYY-MM-DD.md`).

- Localhost only by default (refuses to bind to anything outside `127.0.0.0/8`
  / `::1`).
- Optional `-docker-network NAME` binds instead to the host-side gateway of a
  named docker bridge so devcontainers on that bridge can reach the server
  without giving them host network access. See [Devcontainer access](#devcontainer-access).
- Streamable HTTP transport on `127.0.0.1:17310` (Cursor's MCP client does
  not yet support unix domain sockets; loopback TCP is the next-best option).
- Creates the daily file and the section if either is missing.
- Bullets are timestamped `- HH:MM — text` by default; pass `-no-timestamp`
  to disable.
- Concurrent writes are serialised in-process and committed atomically via
  `rename(2)` to keep Obsidian's file watcher happy.

## Install / refresh

`./deploy.sh` builds from the current checkout, installs the binary into
`~/.local/bin/` and the unit into `~/.config/systemd/user/`, reloads user
systemd, and either enables-and-starts or restarts the service depending on
whether it's already enabled. Re-run after every code change.

```bash
cd ~/Projects/journal-mcp
./deploy.sh
```

Sanity-check:

```bash
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

## Devcontainer access

Cursor (or any other MCP client) running inside a devcontainer can't reach
`127.0.0.1:17310` on the host — the container has its own loopback. Rather
than ship `--network=host` to a shared devcontainer, run `journal-mcp` with
`-docker-network <bridge>` and it will bind to that bridge's host-side
gateway instead. From any container attached to the same docker network,
the host is then reachable at the default route:

```bash
# Inside the devcontainer:
HOST_IP=$(ip route | awk '/default/ {print $3}')
curl -i "http://${HOST_IP}:17310/"
```

The default unit uses `-docker-network overmind_default`. If your compose
project uses a different network name, edit the unit (or override with
`systemctl --user edit journal-mcp.service`).

Safety notes:

- The resolved gateway must be RFC1918 (or loopback). A docker network with
  a public-IP gateway is refused at startup.
- The gateway is queried from the docker engine over its unix socket
  (`/var/run/docker.sock`), so the user running the service must be in the
  `docker` group. No `docker` CLI dependency.
- The bridge interface is not advertised on the LAN — only containers on
  that specific docker network can reach the listener, same blast radius
  as the existing loopback-only mode plus those containers.
- If the named network doesn't exist yet (docker not started, compose not
  brought up), the service exits non-zero and systemd retries every 2s.

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
| `-addr`             | `127.0.0.1:17310`                             | Loopback address to listen on. Non-loopback is refused unless `-docker-network` is set, in which case only the port is used. |
| `-docker-network`   | _(unset)_                                     | If set, bind to the host-side gateway of this docker bridge network. Must resolve to an RFC1918 / loopback IP. |
| `-docker-socket`    | `/var/run/docker.sock`                        | Path to the docker engine socket. Used only with `-docker-network`. |
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
