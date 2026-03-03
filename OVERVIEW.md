### `plugin-wiz` repository

#### Project Overview

This repository contains the `plugin-wiz`, a plugin that integrates the Slidebolt system with WiZ smart lights. It allows for the discovery, monitoring, and control of WiZ devices on the local network.

#### Architecture

The `plugin-wiz` is a Go application that communicates with WiZ lights using their UDP-based local communication protocol.

-   **UDP Discovery**: The plugin discovers WiZ lights on the network by sending out UDP broadcast probes and listening for announcement messages from the bulbs. It can also be configured with a static list of IP addresses to probe.

-   **Device and Entity Creation**: For each discovered WiZ bulb, the plugin creates a corresponding `light` entity within the Slidebolt system.

-   **Local Control**: All communication happens on the local network, without relying on the WiZ cloud. The plugin sends UDP packets directly to the IP address of each bulb to control it.

-   **Command Handling**: The plugin translates standard Slidebolt `light` commands (like `turn_on`, `set_brightness`, `set_rgb`) into the specific JSON-based UDP payloads that the WiZ bulbs understand.

#### Key Files

| File | Description |
| :--- | :--- |
| `main.go` | The main entry point that initializes and runs the plugin. |
| `plugin.go` | The core plugin logic, which implements the `runner.Plugin` interface. It manages the discovery of WiZ bulbs, handles their state, and processes commands. |
| `wiz_client.go`| Contains the low-level implementation of the WiZ UDP protocol, including functions for sending discovery probes, listening for responses, and sending control commands. |

#### Available Commands

The plugin supports the standard commands for the `light` domain, which are translated into WiZ UDP commands:

-   `turn_on`
-   `turn_off`
-   `set_brightness`
-   `set_rgb`
-   `set_temperature`
-   `set_scene`

#### Standalone Discovery Mode

This plugin supports a standalone discovery mode for rapid testing and diagnostics without requiring the full Slidebolt stack (NATS, Gateway, etc.).

To run discovery and output the results to JSON:
```bash
./plugin-wiz -discover
```

**Note**: Ensure any required environment variables (e.g., API keys, URLs) are set before running.
