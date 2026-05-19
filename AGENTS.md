# AGENTS.md

Repo-local overrides to my home-dir agent rules (`~/AGENTS.md`, `~/CLAUDE.md`).

## Git workflow

- **Push straight to `main`.** This is a personal single-maintainer repo; no branch/PR ceremony needed. The global "never push to `dev`" rule still applies (this repo just doesn't have a `dev` branch).
- Commit subject in imperative mood, no body unless the change actually needs one.
- After any code change that affects the running server, re-run `./deploy.sh` so the systemd unit picks up the new binary.
