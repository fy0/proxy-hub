<div align="center">
  <h1>ProxyHub</h1>

  <img src="media/readme/proxyhub-pool-flow-en.png" alt="ProxyHub proxy format flow" width="680">

  <p><strong>Simple proxy format conversion tool</strong></p>
  <p>Import common proxy links and turn them into ready-to-use local SOCKS5/HTTP endpoints.</p>

  <p>
    <a href="README.md">English</a> ·
    <a href="README.zh-CN.md">简体中文</a>
  </p>
</div>

## What It Solves

ProxyHub focuses on everyday proxy link handling: import, preview, convert, probe, and expose stable local SOCKS5/HTTP ports. It can run multiple local HTTP/SOCKS proxies at the same time. Built on sing-box.

## Features

| Feature | Why it matters |
| --- | --- |
| Format conversion | Import common proxy links (VLESS, VMess, Trojan, Shadowsocks, Hysteria/Hysteria2, TUIC, SSH, SOCKS5, HTTP) and output local proxy endpoints. |
| Smart routing | Prefer low latency, fail over, balance, or switch manually. |
| Health guard | Probe latency and automatically exclude broken routes. |
| Chain nodes | Chain multiple nodes into one ordered proxy path, with traffic forwarded through each node. |
| Bulk import | Paste links or subscriptions, preview, then import. |
| Backup | Move the proxy setup with one JSON file. |

## Quick Start

### npm

```bash
npm install -g proxy-hub
proxy-hub
```

Then open:

```text
http://127.0.0.1:3020
```

Use the same command to update to the latest stable release:

```bash
npm install -g proxy-hub@latest
```

### Docker

```bash
docker run -d --name proxyhub -p 3020:3020 -v proxyhub-data:/app/data ghcr.io/fy0/proxy-hub:latest
```

Then open:

```text
http://127.0.0.1:3020
```

### Binary

Download the latest archive from [GitHub Releases](https://github.com/fy0/proxy-hub/releases), extract it, then run `proxy-hub` or `proxy-hub.exe`.

## Screenshots

### Local Ports

<img src="media/readme/proxyhub-local-ports-en.png" alt="Local ports" width="860">

### Add Node

<img src="media/readme/proxyhub-add-node-en.png" alt="Add node" width="860">

### Chain Node

<img src="media/readme/proxyhub-chain-node-en.png" alt="Add chain node" width="860">

### Batch Import

<img src="media/readme/proxyhub-batch-import-en.png" alt="Batch import nodes" width="860">

## Configuration

ProxyHub reads runtime settings from `data/config.yaml`.

Common keys:

| Key | Purpose |
| --- | --- |
| `serveAt` | Service listen address, default `:3020`. |
| `dbUrl` | Database DSN, default `./data/data.db`. |
| `logLevel` | Service log level. |

SQLite, PostgreSQL, and MySQL DSNs are supported.

## License

ProxyHub is distributed under GPL-3.0-or-later because the released
application links against SagerNet sing/sing-box.
