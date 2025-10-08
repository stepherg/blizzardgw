# Blizzard Gateway

A WebSocket-to-WRP gateway service that bridges JSON-RPC 2.0 clients with XMiDT/WRP-enabled devices (BlizzardRDK). The gateway provides a stable JSON-RPC API surface for control-plane applications while handling WRP message translation, device routing, and event fanout.

## Overview

Blizzard Gateway acts as a protocol translation layer between:

- **Clients**: External applications communicating via WebSocket with JSON-RPC 2.0
- **Devices**: BlizzardRDK runtimes connected through the XMiDT fabric (Scytale/Talaria)

### Key Features

- **WebSocket JSON-RPC 2.0 API**: Standards-based client interface
- **WRP Protocol Bridge**: Translates JSON-RPC to/from Web Routing Protocol messages
- **Event Fanout**: Distributes device-originated events to connected clients
- **Webhook Integration**: Registers with Argus for device event notifications
- **Multi-Service Fallback**: Attempts delivery across multiple service endpoints
- **Flexible Configuration**: Environment-based setup for various deployment scenarios

## Architecture

```text
┌─────────┐              ┌──────────────┐              ┌─────────┐
│ Client  │──JSON-RPC───▶│   Blizzard   │────WRP──────▶│ Scytale │
│ (WebSoc)│◀─────────────│   Gateway    │◀─────────────│         │
└─────────┘              └──────────────┘              └─────────┘
                                │                            │
                                │ Webhook                    │
                                │ Events                     ▼
                         ┌──────┴────────┐            ┌──────────┐
                         │     Argus     │            │  Device  │
                         │  (Webhooks)   │            │ (BlizRDK)│
                         └───────────────┘            └──────────┘
```

### Request/Response Flow

1. Client sends JSON-RPC request over WebSocket
2. Gateway wraps request into WRP `SimpleRequestResponse` message
3. WRP message sent to Scytale with device destination (`mac:<device>/<service>`)
4. Device processes request and returns WRP response
5. Gateway extracts JSON-RPC response from WRP payload
6. Response returned to client with matching request ID

### Event Notification Flow

1. Device publishes event through XMiDT fabric
2. Argus webhook delivers event to gateway `/webhook/events` endpoint
3. Gateway publishes event to internal event bus
4. Event broadcast as JSON-RPC notification to connected clients (no ID field)

## Quick Start

### Build

```bash
go build -o blizzardgw ./cmd/blizzardgw
```

### Run

Basic usage:
```bash
./blizzardgw -listen :8920
```

With WRP bridging enabled:
```bash
export SCYTALE_URL=http://scytale:6300/api/v2/device
export SCYTALE_AUTH=dXNlcjpwYXNz
./blizzardgw -listen :8920
```

### Docker

```dockerfile
FROM golang:1.24 AS builder
WORKDIR /app
COPY . .
RUN go build -o blizzardgw ./cmd/blizzardgw

FROM alpine:latest
RUN apk --no-cache add ca-certificates
COPY --from=builder /app/blizzardgw /usr/local/bin/
ENTRYPOINT ["blizzardgw"]
CMD ["-listen", ":8920"]
```

## Configuration

### Command Line Flags

- `-listen`: HTTP listen address (default: `:8920`)

### Environment Variables

#### Core Settings

| Variable | Description | Default |
|----------|-------------|---------|
| `SCYTALE_URL` | Scytale WRP endpoint URL | `http://scytale:6300/api/v2/device` |
| `SCYTALE_AUTH` | Authorization header value (base64) | `dXNlcjpwYXNz` |

#### Webhook Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `WEBHOOK_ENABLE` | Enable webhook registration | `true` |
| `ARGUS_URL` | Argus webhook service URL | `http://argus:6600` |
| `ARGUS_BASIC_AUTH` | Basic auth for Argus (with `Basic` prefix) | `Basic dXNlcjpwYXNz` |
| `ARGUS_BUCKET` | Argus bucket name | `webhooks` |
| `WEBHOOK_URL` | Callback URL for webhook events | `http://blizzardgw:8920/webhook/events` |
| `WEBHOOK_EVENTS` | Event regex pattern | `.*` |
| `WEBHOOK_DEVICE_MATCH` | Device regex pattern | `.*` |
| `WEBHOOK_TTL` | Webhook registration TTL (seconds) | `86400` (24 hours) |
| `WEBHOOK_MAX_RETRIES` | Max retry attempts | `3` |

#### Device Routing

| Variable | Description | Default |
|----------|-------------|---------|
| `DEST_PREFIX` | Device ID prefix for WRP destination | `mac:` |
| `CANONICAL_SERVICE_NAME` | Primary service name for routing | `BlizzardRDK` |
| `DEST_SERVICE_FALLBACKS` | Comma-separated fallback services | (none) |

### Example Configuration

```bash
# Production configuration
export SCYTALE_URL=https://scytale.prod.example.com/api/v2/device
export SCYTALE_AUTH=$(echo -n "user:password" | base64)
export ARGUS_URL=https://argus.prod.example.com
export WEBHOOK_URL=https://blizzardgw.prod.example.com/webhook/events
export WEBHOOK_TTL=3600
export DEST_SERVICE_FALLBACKS=iot-agent,device-manager

./blizzardgw -listen :8920
```

## API Reference

### WebSocket Endpoints

#### Primary Endpoint

```text
ws://localhost:8920/<device>/<service>
```

- `<device>`: Device identifier (e.g., MAC address)
- `<service>`: Service name (used for logging; canonical service used for routing)

**Example:**

```text
ws://localhost:8920/112233445566/BlizzardRDK
```

Uses echo dispatcher (no WRP bridging)

### JSON-RPC 2.0 Protocol

#### Request Format

```json
{
  "jsonrpc": "2.0",
  "id": "uuid-or-string",
  "method": "device.method",
  "params": {
    "key": "value"
  }
}
```

#### Response Format

```json
{
  "jsonrpc": "2.0",
  "id": "uuid-or-string",
  "result": {
    "data": "response"
  }
}
```

#### Error Format

```json
{
  "jsonrpc": "2.0",
  "id": "uuid-or-string",
  "error": {
    "code": -32100,
    "message": "Transport error",
    "data": "Additional context"
  }
}
```

#### Gateway Error Codes

| Code | Description |
|------|-------------|
| `-32100` | WRP transport error (HTTP non-2xx from Scytale) |
| `-32603` | Internal JSON-RPC error (marshal/unmarshal failure) |

Device-originated errors pass through unchanged.

#### Notification Format (Events)

```json
{
  "jsonrpc": "2.0",
  "method": "event.name",
  "params": {
    "device": "112233445566",
    "service": "BlizzardRDK",
    "payload": {}
  }
}
```

Note: Notifications have no `id` field (server-initiated push).

### Webhook Endpoint

```http
POST /webhook/events
Content-Type: application/json
```

Accepts device events from Argus webhook delivery.

**JSON Payload:**
```json
{
  "device": "mac:112233445566",
  "service": "BlizzardRDK",
  "name": "device.event",
  "payload": {}
}
```

**Fallback Headers** (for non-JSON payloads):

- `X-Xmidt-Device` or `X-Device-ID`: Device identifier
- `X-Service`: Service name
- `X-Event-Name`: Event name

## Development

### Project Structure

```text
blizzardgw/
├── cmd/
│   └── blizzardgw/
│       └── main.go          # Entry point, wiring, config
├── internal/
│   ├── config/              # Configuration structures
│   ├── events/              # Internal event bus
│   ├── rpc/                 # JSON-RPC and WRP dispatchers
│   ├── webhook/             # Webhook registration & handling
│   └── ws/                  # WebSocket handler & client logic
├── docs/
│   └── blizzard_gateway.md  # Detailed specification
├── go.mod
└── README.md
```

### Running Tests

```bash
go test ./...
```

Run specific package tests:


```bash
go test ./internal/rpc/
go test ./internal/ws/
```

### Dependencies

Key dependencies (see `go.mod` for full list):

- `gorilla/websocket`: WebSocket implementation
- `xmidt-org/wrp-go/v3`: WRP protocol library
- `xmidt-org/ancla`: Webhook registration client
- `xmidt-org/argus`: Webhook service integration

## Operational Considerations

### Logging

Structured logs include:

- Connection events (device, service, destination)
- Request/response correlation (transaction IDs)
- Webhook event ingestion (device, event name, payload size)
- Error conditions with context

### Health Checks

The gateway listens on the configured port. Basic health check:
```bash
curl -v http://localhost:8920/
```

WebSocket upgrade attempt indicates service is running.

### Scaling

- **Stateless Design**: Gateway instances can be horizontally scaled
- **Per-Connection State**: Each WebSocket maintains its own send queue
- **Event Fanout**: All connected clients receive broadcast notifications

### Security Considerations

**Current State (Development):**
- No authentication on WebSocket connections
- Basic auth for Argus webhook registration
- CORS allows all origins (`CheckOrigin` returns true)

**Planned Enhancements:**
- Bearer token / OIDC authentication
- Per-method authorization policies
- mTLS support
- Webhook signature validation (HMAC/JWT)

## Troubleshooting

### Common Issues

**WebSocket connection fails:**
- Verify gateway is listening: `netstat -an | grep 8920`
- Check CORS settings if connecting from browser
- Review logs for upgrade errors

**No response from device:**
- Confirm `SCYTALE_URL` is correct and reachable
- Verify device is online and registered with XMiDT
- Check device/service routing: ensure `CANONICAL_SERVICE_NAME` matches device registration
- Review WRP message logs on Scytale/Talaria

**Webhook events not received:**
- Verify Argus registration succeeded (check startup logs)
- Confirm `WEBHOOK_URL` is reachable from Argus
- Check Argus logs for delivery attempts
- Validate event/device match patterns

**Multi-service fallback not working:**
- Enable with `DEST_SERVICE_FALLBACKS=service1,service2`
- Check logs for "multi-service fallback enabled" message
- Verify fallback services are registered with device

### Debug Mode

Increase log verbosity by reviewing webhook debug logs:
```
webhook.debug ts=... device=... service=... payload_bytes=...
```

## Roadmap

### Completed ✓

- JSON-RPC 2.0 WebSocket interface
- WRP protocol bridging
- Event bus and notification fanout
- Argus webhook integration
- Multi-service fallback support

### Planned

- [ ] Authentication (Bearer/OIDC)
- [ ] Authorization policies (method allow lists)
- [ ] Metrics (Prometheus)
- [ ] Structured logging (JSON output)
- [ ] Rate limiting per client/device
- [ ] Batch JSON-RPC support
- [ ] Multi-device multiplexing on single WebSocket
- [ ] Request/response timeout configuration
- [ ] Health check endpoint

## License

See [LICENSE](LICENSE) file for details.

## Contributing

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## References

- [XMiDT Project](https://xmidt.io/)
- [WRP Specification](https://github.com/xmidt-org/wrp-c)
- [JSON-RPC 2.0 Specification](https://www.jsonrpc.org/specification)
- [Detailed Gateway Specification](docs/blizzard_gateway.md)