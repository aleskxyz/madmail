# Proxy Architecture

Madmail ships built-in proxy transports that restrict traffic to only the
local mail services (IMAP, SMTP, TURN). This document describes the transport
modes, TLS configuration, admin API, and client compatibility.

> **⚠️ Not a general-purpose proxy.** All transports enforce a port whitelist
> (`ss_allowed_ports`). Only connections to configured local services are
> permitted. All external traffic is rejected.

## Transport Overview

| Transport          | Protocol Stack                        | Default Port   | Clients                                | DB Toggle Key             |
|--------------------|---------------------------------------|----------------|----------------------------------------|---------------------------|
| Shadowsocks (TCP)  | SS cipher over raw TCP                | `ss_addr`      | **Delta Chat**, sslocal                | `__SS_ENABLED__`          |
| Shadowsocks (gRPC) | SS cipher → gRPC frames → TLS        | `ss_addr + 1`  | v2rayN, v2rayNG, Shadowrocket          | `__SS_GRPC_ENABLED__`     |
| Shadowsocks (WS)   | SS cipher → WS frames → TLS          | `ss_addr + 2`  | Delta Chat (WS mode), v2ray-plugin     | `__SS_WS_ENABLED__`       |
| HTTP CONNECT       | HTTP CONNECT + Basic Auth over TLS    | HTTPS port     | Any HTTP proxy client                  | `__HTTP_PROXY_ENABLED__`  |

Each transport can be **individually enabled or disabled** via the admin panel
or the Admin API. Disabled transports do not start and their URLs return empty.

## Data Flow

```
Client (Delta Chat / sslocal / v2rayN)
  │
  ├─ Raw TCP ─────────────────────────→ :8388  go-shadowsocks2 ─→ 127.0.0.1:{993,465,...}
  │
  ├─ gRPC+TLS ──→ :8389  Xray-core ──→ :8388  go-shadowsocks2 ─→ 127.0.0.1:{993,465,...}
  │
  ├─ WS+TLS ───→ :8390  Xray-core ──→ :8388  go-shadowsocks2 ─→ 127.0.0.1:{993,465,...}
  │
  └─ HTTP CONNECT → :443/proxy ───────→ 127.0.0.1:{993,465,...}  (direct TCP tunnel)
```

The Xray-core instances are **transparent byte-stream tunnels** — they strip
the transport framing (gRPC or WebSocket) and forward raw bytes to the SS
handler. The SS encryption/decryption happens at the client and SS handler.

## TLS Configuration

All TLS-based transports (gRPC, WebSocket, HTTP CONNECT) use the **same
TLS certificates** as the main chatmail endpoint. The resolution order is:

1. Explicit `ss_cert` / `ss_key` directives in the config file
2. `{state_dir}/certs/fullchain.pem` and `{state_dir}/certs/privkey.pem`
3. Fallback: `/etc/maddy/certs/fullchain.pem` and `/etc/maddy/certs/privkey.pem`

This is implemented in `resolveSSTlsPaths()` in `chatmail.go`. The same
certificate files are shared with the proxy_protocol TLS listener where
applicable (see `internal/proxy_protocol/proxy_protocol.go`).

The proxy_protocol module supports wrapping any listener with PROXY protocol
v1/v2 headers and optional TLS, using a configurable trust list for source
IP verification:

```go
// proxy_protocol.go
ProxyProtocol{
    trust:     []net.IPNet,   // trusted source CIDRs
    tlsConfig: *tls.Config,   // TLS config from the 'tls' directive
}
```

## SIP002 URL Format

All Shadowsocks URLs follow [SIP002](https://github.com/shadowsocks/shadowsocks-org/wiki/SIP002-URI-Scheme):

```
ss://BASE64(method:password)@host:port[/?plugin=...][#tag]
```

### Raw TCP (Delta Chat default)

```
ss://YWVzLTEyOC1nY206cGFzcw@example.com:8388#example.com
```

No `?plugin=` parameter. This is the format chatmail-core has always supported.

### gRPC + TLS (v2ray-plugin clients)

```
ss://YWVzLTEyOC1nY206cGFzcw@example.com:8389/?plugin=v2ray-plugin%3Bmode%3Dgrpc%3Bhost%3Dexample.com#example.com
```

Plugin value (decoded): `v2ray-plugin;mode=grpc;host=example.com`

### WebSocket + TLS (Delta Chat WS mode / v2ray-plugin)

```
ss://YWVzLTEyOC1nY206cGFzcw@example.com:8390/?plugin=v2ray-plugin%3Bmode%3Dwebsocket%3Bhost%3Dexample.com%3Bpath%3D%2Fss%3Btls#example.com
```

Plugin value (decoded): `v2ray-plugin;mode=websocket;host=example.com;path=/ss;tls`

## HTTP CONNECT Proxy

The HTTP CONNECT proxy operates on the **HTTPS port** (default 443) at a
configurable path (default `/proxy`). It uses HTTP Basic authentication.

### Configuration

| Setting                 | DB Key                    | Default     | Description                    |
|-------------------------|---------------------------|-------------|--------------------------------|
| Enabled                 | `__HTTP_PROXY_ENABLED__`  | `disabled`  | Enable/disable the proxy       |
| Port                    | `__HTTP_PROXY_PORT__`     | HTTPS port  | Port to listen on              |
| Path                    | `__HTTP_PROXY_PATH__`     | `/proxy`    | URL path on the web server     |
| Username                | `__HTTP_PROXY_USERNAME__` | `madmail`   | Basic auth username            |
| Password                | `__HTTP_PROXY_PASSWORD__` | (none)      | Basic auth password            |

### Usage

```bash
curl -x https://user:pass@example.com:443/proxy https://127.0.0.1:993
```

The proxy only allows CONNECT to localhost ports listed in `ss_allowed_ports`.

## Client Compatibility

### chatmail-core (Delta Chat's Rust core)

chatmail-core uses the `shadowsocks` Rust crate (v1.23.x). It supports:

- **Raw TCP** (always worked) — uses `ss://` URL without plugin
- **WebSocket + TLS** (new) — parses `?plugin=v2ray-plugin;mode=websocket;...`
  from the URL, establishes TCP → TLS → WebSocket, then runs SS protocol inside
  WS binary frames via `WsStreamAdapter`

> **Note:** gRPC transport is **not** supported in chatmail-core. gRPC URLs
> are only for external Shadowsocks clients (v2rayN, etc.).

### External clients (v2rayN, Shadowrocket, sslocal)

These clients use `v2ray-plugin` as an external binary and support all three
transport modes. Use the appropriate SIP002 URL for your client.

## Admin API

All proxy settings are manageable via the Admin API.

### Toggle Endpoints (POST)

| Endpoint                        | Controls                  |
|---------------------------------|---------------------------|
| `/admin/services/shadowsocks`   | SS raw TCP                |
| `/admin/services/ss_ws`         | SS WebSocket transport    |
| `/admin/services/ss_grpc`       | SS gRPC transport         |
| `/admin/services/http_proxy`    | HTTP CONNECT proxy        |

Request body:
```json
{"action": "toggle"}
```

### Setting Endpoints (GET/POST)

| Endpoint                              | Key                         |
|---------------------------------------|-----------------------------|
| `/admin/settings/ss_port`             | `__SS_PORT__`               |
| `/admin/settings/ss_password`         | `__SS_PASSWORD__`           |
| `/admin/settings/ss_cipher`           | `__SS_CIPHER__`             |
| `/admin/settings/ss_ws_port`          | `__SS_WS_PORT__`            |
| `/admin/settings/ss_grpc_port`        | `__SS_GRPC_PORT__`          |
| `/admin/settings/http_proxy_port`     | `__HTTP_PROXY_PORT__`       |
| `/admin/settings/http_proxy_path`     | `__HTTP_PROXY_PATH__`       |
| `/admin/settings/http_proxy_username` | `__HTTP_PROXY_USERNAME__`   |
| `/admin/settings/http_proxy_password` | `__HTTP_PROXY_PASSWORD__`   |

All settings are also returned in the bulk `GET /admin/settings` response.

### Configuration Priority

1. **Database** (highest) — set via admin panel or API
2. **Config file** — `maddy.conf` directives
3. **Defaults** — hardcoded fallbacks

## Server Configuration

In the maddy config file:

```
chatmail ... {
    ss_addr    0.0.0.0:8388    # Raw TCP SS port
    ss_password your-password
    ss_cipher  aes-128-gcm     # Default cipher

    # Optional: TLS cert/key for gRPC and WS transports
    # Defaults to {state_dir}/certs/fullchain.pem and privkey.pem
    ss_cert /path/to/fullchain.pem
    ss_key  /path/to/privkey.pem

    # Optional: restrict which local ports SS can reach
    ss_allowed_ports 25 143 465 587 993 3478 5349
}
```

Ports are assigned automatically:
- `ss_addr` → raw TCP
- `ss_addr + 1` → gRPC + TLS (overridable via `__SS_GRPC_PORT__`)
- `ss_addr + 2` → WebSocket + TLS (overridable via `__SS_WS_PORT__`)

## Startup Behavior

Each transport checks its DB toggle before starting:

```go
// chatmail.go — Init()
if e.isGrpcEnabled() {
    go e.runXrayGRPC(grpcPort)
}
if e.isWsEnabled() {
    go e.runXrayWS(wsPort)
}
```

Toggling a transport **requires a restart** to take effect (the Xray instances
are started once during `Init()`). URL generation functions check the DB on
every request, so disabled transports immediately stop showing URLs on the
info page.

## Security Notes

1. **Port whitelist**: The SS handler and HTTP proxy only allow connections to
   configured local ports. They are **not** general-purpose proxies.
2. **TLS certificates**: The gRPC, WS, and HTTP CONNECT transports use the
   server's TLS certificates. Traffic is indistinguishable from normal
   HTTPS/gRPC/WS traffic.
3. **No plugin execution**: chatmail-core does NOT execute `v2ray-plugin` as
   an external binary. The `?plugin=` URL parameter is used solely to determine
   the transport mode. The WS handshake and framing are handled natively.
4. **HTTP proxy auth**: The HTTP CONNECT proxy requires Basic authentication.
   Unauthenticated requests are rejected with 407 Proxy Authentication Required.
