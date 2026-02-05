# gognestcli — Nest cameras in your terminal.

CLI for Google Nest cameras via the Smart Device Management (SDM) API. Single binary, secure credential storage, WebRTC streaming.

## Features

- **Auth** — OAuth2 browser flow with local callback (or `--manual` paste for headless/SSH)
- **Devices** — List all Nest devices in your SDM project
- **Info** — Show camera traits, status, and room assignment
- **Snapshot** — Capture a JPEG frame from a live camera stream (via WebRTC + ffmpeg)
- **Record** — Record MP4/WebM video clips of any duration
- **Live** — Low-latency live view window via ffplay
- **Stream** — Raw H264 to stdout — pipe to any player or tool
- **Events** — Listen for motion/person events via Pub/Sub, auto-capture snapshots and clips on trigger
- **Secure credentials** — Refresh tokens stored in OS keyring (macOS Keychain, Linux SecretService), never plaintext on disk

## Installation

### Build from Source

Requires Go 1.21+ and optionally ffmpeg for JPEG snapshots.

```bash
git clone https://github.com/gdaybrice/gognestcli.git
cd gognestcli
go build -o gognestcli .
```

### Optional: ffmpeg

Required for snapshots, recording, and live view:

```bash
brew install ffmpeg    # macOS
sudo apt install ffmpeg # Linux
```

## Quick Start

### 1. Google Cloud Setup

1. Create a project in the [Google Cloud Console](https://console.cloud.google.com/projectcreate)
2. Enable the [Smart Device Management API](https://console.cloud.google.com/apis/api/smartdevicemanagement.googleapis.com)
3. Create an [SDM project](https://console.nest.google.com/device-access) ($5 one-time fee)
4. Create an OAuth 2.0 Client ID (Application type: **Web application**)
5. Add `http://localhost:9004/callback` as an **Authorized redirect URI**
6. Link your Nest account in the [Device Access Console](https://console.nest.google.com/device-access)

### 2. Authenticate

```bash
./gognestcli auth
```

You'll be prompted for your Client ID, Client Secret, and SDM Project ID (saved to `~/.config/gognestcli/config.json`). Then a browser window opens for OAuth authorization.

For headless environments:

```bash
./gognestcli auth --manual
```

### 3. Use

```bash
# List cameras
./gognestcli devices

# Show camera details
./gognestcli info

# Take a snapshot
./gognestcli snapshot -o photo.jpg

# Record 15 seconds of video
./gognestcli record -d 15 -o clip.mp4

# Live view window
./gognestcli live

# Stream raw H264 to stdout (pipe to any player)
./gognestcli stream | ffplay -f h264 -

# Listen for events and auto-capture
./gognestcli events -o ./captures

# Events with video clips on motion
./gognestcli events -o ./captures --clip --clip-secs 10
```

## Commands

```
gognestcli auth [--manual]                  # OAuth setup
gognestcli devices                          # List devices
gognestcli info [device-id]                 # Camera traits + status
gognestcli snapshot [-o file.jpg]           # Snapshot (JPEG via WebRTC + ffmpeg)
gognestcli record [-d 15] [-o clip.mp4]     # Record N seconds to MP4/WebM
gognestcli live [-d device-id]              # Live view via ffplay
gognestcli stream [-d device-id]            # Raw H264 to stdout
gognestcli events [-o dir] [--clip]         # Auto-capture on motion/person events
gognestcli version                          # Print version
```

## Configuration

### Config file

`~/.config/gognestcli/config.json`:

```json
{
  "client_id": "...",
  "client_secret": "...",
  "project_id": "...",
  "device_id": "enterprises/.../devices/...",
  "pubsub_subscription": "projects/.../subscriptions/..."
}
```

`device_id` and `pubsub_subscription` are optional — commands auto-detect the first camera when omitted.

### Tokens

Refresh tokens are stored in the OS keyring via [99designs/keyring](https://github.com/99designs/keyring):

- **macOS**: Keychain
- **Linux**: SecretService (GNOME Keyring, KWallet) or encrypted file fallback

Never written as plaintext to disk.

## How It Works

- **WebRTC streaming** via [Pion](https://github.com/pion/webrtc) — pure Go, no browser needed
- **H264 video + Opus audio** — received as RTP, written as raw H264 Annex B
- **ffmpeg pipeline** — raw H264 → JPEG snapshots, MP4/WebM clips, or piped to ffplay for live view
- **Event images** — fast JPEG download via CameraEventImage API (no WebRTC needed per event)
- **Event polling** — Pub/Sub REST API (`pull` + `acknowledge`), triggers snapshot/clip on motion or person detection
- **Stream management** — auto-extends WebRTC session every 4 minutes, sends PLI every 2 seconds for keyframes

## Security

- OAuth client credentials stored in config file with `0600` permissions
- Refresh tokens in OS keyring only
- Config directory created with `0700` permissions
- Never commit `config.json` to version control (included in `.gitignore`)

## Dependencies

- [kong](https://github.com/alecthomas/kong) — CLI framework
- [99designs/keyring](https://github.com/99designs/keyring) — OS keyring
- [pion/webrtc](https://github.com/pion/webrtc) — pure Go WebRTC
- [pion/rtcp](https://github.com/pion/rtcp) — RTCP for PLI requests
- **ffmpeg** (system binary) — video conversion, snapshots, live view

## Credits

Inspired by [gogcli](https://github.com/steipete/gogcli) patterns (Kong CLI, keyring storage, browser OAuth flow).

## License

MIT
