# Peer-Drop

[![MIT License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Peer-Drop** is a local-network and cross-network P2P file sharing tool. Run it on any computer, open a browser, and instantly send files to other devices on the same network — no accounts, no cloud, no app install required. File transfers happen directly between browsers via WebRTC.

## How It Works

1. Run `peer-drop` on any computer
2. Open `http://localhost:8080` in any browser (desktop or phone)
3. Devices on the same network automatically discover each other
4. Click on a device to send files — transfer goes peer-to-peer via WebRTC

Public rooms are also supported for sharing across different networks.

## Prerequisites

- **Go 1.21+** (module uses `go 1.25.5` toolchain)

Check your version:

```bash
go version
```

## Installation & Build

```bash
git clone https://github.com/Seiya380/Peer-Drop.git
cd Peer-Drop

# Build binary
go build -o peer-drop .

# Or run directly
go run .
```

## Usage

```bash
# Start with default port (8080)
./peer-drop

# Start on a custom port
./peer-drop -port 9000

# Enable verbose logging
./peer-drop -verbose

# Show version
./peer-drop -version

# Show help
./peer-drop -help
```

Then open the URL shown in the terminal in your browser. Other devices on the same network can connect via `http://<your-ip>:<port>`.

## Architecture

```
Peer-Drop/
├── main.go                  # Entry point, CLI flags, graceful shutdown
├── go.mod / go.sum          # Go modules (gorilla/websocket)
├── internal/
│   ├── config/              # Configuration loading
│   ├── server/              # HTTP server, routes
│   └── signaling/           # WebRTC signaling over WebSockets
└── web/
    ├── static/              # Frontend assets (JS, CSS)
    ├── templates/           # HTML templates
    └── embed.go             # Go embed for bundling web assets
```

The server acts as a **signaling relay** only — once two peers have exchanged WebRTC offers/answers via WebSocket, the actual file data flows directly browser-to-browser (no server in the data path).

## License

This project is licensed under the [MIT License](LICENSE).
