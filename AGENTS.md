# Repository Guidelines

## Project Structure

- `main.go`: CLI entrypoint → `cmd.Execute()`.
- `internal/cmd/`: Kong-based CLI commands (auth, devices, info, snapshot, record, live, stream, events).
- `internal/config/`: JSON config at `~/.config/gognestcli/config.json`.
- `internal/secrets/`: OS keyring via `99designs/keyring` for refresh token storage.
- `internal/auth/`: OAuth2 flow (browser callback + manual paste) and token refresh.
- `internal/sdm/`: SDM REST API client (no googleapis SDK). Includes WebRTC stream management and event image download.
- `internal/webrtc/`: Pion WebRTC session management for camera streams.
- `internal/recorder/`: Raw H264 capture + ffmpeg pipeline for JPEG/MP4/WebM conversion. Also provides stdout and pipe writers.
- `internal/pubsub/`: Pub/Sub REST API polling for device events.

## Build & Development Commands

- `go build -o gognestcli .`: build the binary.
- `go vet ./...`: static analysis.
- `go test ./...`: run tests.
- `ffmpeg` must be in PATH for snapshots, recording, and live view.

## Coding Style

- Formatting: `gofmt` / `goimports`.
- All packages live under `internal/` — nothing is exported for external consumption.
- HTTP clients use standard library `net/http` — no heavyweight SDK dependencies.
- Output: keep stdout clean and parseable; warnings/progress go to stderr via `fmt.Fprintf(os.Stderr, ...)`.

## Testing Guidelines

- Unit tests: stdlib `testing` + `net/http/httptest` for API mocking.
- Integration tests require real Google Cloud credentials and a Nest device — gate behind a build tag (`//go:build integration`).

## Security & Configuration

- Never commit OAuth client credentials, tokens, or `config.json`.
- Refresh tokens are stored exclusively in the OS keyring (macOS Keychain, Linux SecretService).
- Config file permissions are `0600`; config directory is `0700`.
- The `--manual` auth flag exists for headless/SSH environments without a browser.
