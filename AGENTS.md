# plugin-wiz Instructions

`plugin-wiz` follows the reference runnable-module architecture.

- Keep `cmd/plugin-wiz/main.go` as a thin wrapper only.
- Put runtime lifecycle and device wiring in `app/`.
- Keep protocol/private helpers under `internal/...`.
- Prefer testing `app/` and `internal/...`; keep `cmd` focused on the BDD/integration harness and temporary compatibility shims only.
