# WiZ Smart Light API Reference

This is a minimal reference implementation showing how to connect to WiZ smart bulbs.

## Connection Overview

**Discovery**: UDP broadcast on port 38899  
**Control**: UDP unicast to each bulb on port 38899  
**Protocol**: JSON payloads (NO encryption)

## Quick Start

```bash
# Set your subnet
echo "WIZ_SUBNET=192.168.88" > .env.local
source .env.local

# Run discovery
go run .
```

## API Commands

### Discovery (get MAC address)
```json
{"method":"getSystemConfig","params":{}}
```

### Get Light State
```json
{"method":"getPilot","params":{}}
```

Response:
```json
{"result":{"state":true,"dimming":50,"r":255,"g":100,"b":50}}
```

### Turn On
```json
{"method":"setPilot","params":{"state":true}}
```

### Turn Off
```json
{"method":"setPilot","params":{"state":false}}
```

### Set Brightness (0-100)
```json
{"method":"setPilot","params":{"state":true,"dimming":75}}
```

### Set RGB Color
```json
{"method":"setPilot","params":{"state":true,"r":255,"g":0,"b":0}}
```

### Set Color Temperature (2200-6500K)
```json
{"method":"setPilot","params":{"state":true,"temp":3000}}
```

### Set Scene (by ID)
```json
{"method":"setPilot","params":{"state":true,"sceneId":11}}
```

Common scene IDs: 1=Ocean, 2=Reading, 3=Rainbow, 11=Warm White

## Environment Variables

```bash
WIZ_SUBNET=192.168.88     # Subnet to scan (required)
WIZ_TIMEOUT_MS=500        # Discovery timeout per device
```

## Key Differences from Kasa

- **No encryption**: WiZ uses plain JSON over UDP
- **Same port for discovery and control**: Port 38899 for both
- **No length headers**: Unlike Kasa's TCP protocol
