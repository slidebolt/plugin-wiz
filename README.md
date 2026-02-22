# Wiz Plugin for Slidebolt

The Wiz Plugin provides integration for WiZ IoT lighting devices. It features automatic discovery via UDP broadcast and real-time control through the Slidebolt Framework.

## Features

- **Automatic Discovery**: Scans the local network for WiZ devices using UDP port 38899.
- **Full Control**: Supports turning lights on/off, brightness adjustment, and color control (RGB/Temperature).
- **Isolated Service**: Designed to run as a standalone sidecar service communicating via NATS.

## Architecture

This plugin follows the Slidebolt "Isolated Service" pattern:
- **`pkg/bundle`**: Implementation of the `sdk.Plugin` interface.
- **`pkg/device`**: Handles the raw UDP communication with WiZ hardware.
- **`cmd/main.go`**: Service entry point.

## Development

### Prerequisites
- Go (v1.25.6+)
- Slidebolt `plugin-sdk` and `plugin-framework` repos sitting as siblings.

### Local Build
Initialize the Go workspace to link sibling dependencies:
```bash
go work init . ../plugin-sdk ../plugin-framework
go build -o bin/plugin-wiz ./cmd/main.go
```

### Testing
```bash
go test ./...
```

## Docker Deployment

### Deployment Requirements
**CRITICAL**: This plugin requires **Host Networking** (`network_mode: host`) to perform UDP broadcast discovery of physical devices on your network.

### Build the Image
To build with local sibling modules (before they are live on GitHub):
```bash
make docker-build-local
```

To build from remote GitHub repositories:
```bash
make docker-build-prod
```

### Run via Docker Compose
Add the following to your `docker-compose.yml`:
```yaml
services:
  wiz:
    image: slidebolt-plugin-wiz:latest
    network_mode: "host"
    environment:
      - NATS_URL=nats://127.0.0.1:24232 # Point to your Core's NATS port
    restart: always
```

## License
Refer to the root project license.
