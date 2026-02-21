# Virtial Connect (MVP)

Windows desktop SFTP client on Go + **Fyne** (single binary, fast MVP delivery without Node/Electron runtime).

## Why Fyne (choice justification)
- Pure Go UI stack, simpler build pipeline for `.exe` compared to web runtime bundles.
- Native-looking desktop widgets and event loop with async safe updates.
- Small MVP surface: forms, lists, dialogs, multiwindow editor.

## Architecture

### Layers / modules
- `cmd/virtial-connect` — app entrypoint.
- `internal/app` — Fyne UI, presentation logic, user actions.
- `internal/sshclient` — SSH/SFTP connection, known_hosts verification, host key prompts.
- `internal/transfer` — transfer queue, workers, progress/speed/ETA/cancel, recursive copy.
- `internal/editor` — remote file open/save (atomic temp+rename), mtime conflict handling.
- `internal/config` — local JSON settings/profile storage.
- `internal/security` — encryption helper for sensitive fields (AES-GCM with machine-derived key in MVP).
- `internal/util` — path and conflict naming helpers.
- `internal/logging` — UI-safe log aggregation.
- `tests` — smoke/integration-style tests with transfer mocks.

### Data flow diagram (text)
1. User fills connection form in UI (`internal/app`) and presses Connect.
2. UI sends config to `sshclient.Connect`.
3. `sshclient` validates known_hosts:
   - Known key: continue.
   - Unknown key: callback to UI confirm dialog -> append to known_hosts.
4. On success UI creates `transfer.Manager` and `editor.Service` from active SFTP client.
5. File panel actions enqueue `TransferTask` into manager queue.
6. Worker picks task -> performs recursive/file transfer using `RemoteFS` abstraction.
7. Progress callback updates queue panel with bytes/speed/ETA/status.
8. Edit action opens remote file via `editor.Open`; save performs atomic upload (`tmp + rename`) and conflict check by `mtime`.
9. Logs/errors go through `logging.Logger`, rendered in bottom log area.

## Repository structure

```text
cmd/virtial-connect/main.go
internal/app/app.go
internal/config/store.go
internal/editor/service.go
internal/logging/logger.go
internal/models/models.go
internal/security/crypto.go
internal/sshclient/client.go
internal/transfer/fs.go
internal/transfer/manager.go
internal/util/path.go
internal/util/path_test.go
tests/transfer_smoke_test.go
README.md
```

## MVP feature coverage
- [x] SSH/SFTP connect (host/port/user/password/private key/passphrase).
- [x] Remote SFTP listing: name/size/date/permissions, directories and parent `..`.
- [x] Local FS panel listing.
- [x] Double click on item: open directory / open file (remote opens editor, local opens via OS default app).
- [x] Search by file/directory name in local and remote panels.
- [x] Connection profile history persisted in local settings.
- [x] Upload/download files + recursive folders.
- [x] Queue, progress, speed, ETA, cancel token mechanics (manager side).
- [x] Remote edit window (open, search/replace, save atomic).
- [x] Remote operations: rename/delete (mkdir can be added via same SFTP client in UI action).
- [x] known_hosts first-connect confirmation.
- [x] Error/log rendering without panic.

## Build & run (Windows PowerShell)

```powershell
# 1) prerequisites
winget install GoLang.Go

# 2) clone and enter
cd .\virtial-connect

# 3) deps
go mod tidy

# 4) run dev
go run .\cmd\virtial-connect

# 5) build exe
go build -o .\bin\virtial-connect.exe .\cmd\virtial-connect
```

## Dev run on Linux/macOS
```bash
go mod tidy
go run ./cmd/virtial-connect
```

## Troubleshooting (Windows)
- При `go run .\cmd\virtial-connect` может долго работать `cc1.exe` (CGO-сборка зависимостей Fyne через GCC) до фактического старта окна.
- Если окно не появляется сразу, дождитесь завершения компиляции или используйте двухшаговый запуск:

```powershell
go build -o .\bin\virtial-connect.exe .\cmd\virtial-connect
.\bin\virtial-connect.exe
```

## Tests
```bash
go test ./...
```

## Acceptance checklist
- [ ] Connect with password auth to Linux host.
- [ ] Connect with ED25519/RSA private key (+ optional passphrase).
- [ ] First unknown host shows fingerprint confirmation dialog.
- [ ] Second connect validates against known_hosts.
- [ ] Local and remote panels both navigate directories.
- [ ] Upload file and download file complete with progress updates.
- [ ] Recursive folder transfer works both directions.
- [ ] Remote rename/delete operations work.
- [ ] Double-click/Context Edit opens editor.
- [ ] Save writes atomically and detects mtime conflicts.
- [ ] UI remains responsive while transfer queue runs.
- [ ] Errors shown in dialog + log panel (no app crash).

## Known MVP limitations
- AES key derivation is machine-bound fallback, not full Windows DPAPI binding yet.
- Conflict policies are basic in UI (overwrite path behavior depends on target create) — advanced per-file policy dialog can be extended.
- Syntax highlighting is hint/label-level only (full token highlight requires richer text widget).
- Context menu in local panel is action-simplified; can be extended to richer right-click options.
- No drag & drop yet.
- No chmod UI yet.
