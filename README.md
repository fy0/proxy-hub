<div align="center">
  <h1>ProxyHub</h1>

  <img src="media/readme/proxyhub-pool-flow-en.png" alt="ProxyHub proxy format flow" width="680">

  <p><strong>Turn your proxy nodes into HTTP/SOCKS5 proxies your apps can use.</strong></p>
  <p>Use VLESS, Hysteria2, or even an SSH server through one local proxy port.</p>

  <p>
    <a href="README.md">English</a> ·
    <a href="README.zh-CN.md">简体中文</a> ·
    <a href="#quick-start">Install</a> ·
    <a href="#screenshots">Screenshots</a>
  </p>
</div>

## What It Solves

Your server apps only support HTTP or SOCKS proxies, but all you have are a few VLESS or Hysteria2 nodes—or just an SSH login. ProxyHub turns them into local HTTP/SOCKS5 proxies, so your apps can use them.

Run multiple local proxy ports at the same time, each connected to one or more proxy nodes. Powered by sing-box.

## Features

| Feature | What it does |
| --- | --- |
| Proxy conversion | VLESS, VMess, Trojan, Shadowsocks, Hysteria/Hysteria2, TUIC, SSH, SOCKS5, HTTP → local HTTP/SOCKS5 proxies. |
| Load balancing / Random | Distribute new connections across nodes in turn, or pick a node at random for each new connection. |
| Node selection | Prefer the lowest latency or select a node manually. Health checks and failover help handle unavailable nodes. |
| Chain nodes | Forward traffic through several nodes in order. |
| Bulk import | Paste proxy links or subscriptions, preview, and import. |
| Backup | Export and restore your setup with one JSON file. |

## Quick Start

### npm

```bash
npm install -g pxhub
pxhub
```

Open [http://127.0.0.1:3020](http://127.0.0.1:3020). The `proxy-hub` command is also available as a compatibility alias.

To update: `npm install -g pxhub@latest`.

### Docker

```bash
docker run -d --name proxyhub -p 3020:3020 -v proxyhub-data:/app/data ghcr.io/fy0/proxy-hub:latest
```

Open [http://127.0.0.1:3020](http://127.0.0.1:3020). To use a proxy port from outside the container, publish that port too (for example, `-p 8080:8080`) and set its listen address to `0.0.0.0` in ProxyHub.

### Binary

Download the latest archive from [GitHub Releases](https://github.com/fy0/proxy-hub/releases), extract it, then run `proxy-hub` or `proxy-hub.exe`.

### Examples: from a node to an app

In the web UI, add a node or paste links into batch import. Replace the example addresses and credentials with your own; URL-encode special characters in usernames and passwords.

**SSH** — with a server address, username, and password:

```text
ssh://alice:your-password@ssh.example.com:22#My-SSH
```

**Hysteria2 (HY2)** — with your node's password and TLS server name:

```text
hy2://your-password@hy2.example.com:443?sni=hy2.example.com#My-HY2
```

**VLESS** — a basic TLS node (use your provider's full link for Reality or other transports):

```text
vless://00000000-0000-4000-8000-000000000001@vless.example.com:443?security=tls&sni=vless.example.com&type=tcp#My-VLESS
```

Create a local HTTP or SOCKS5 port and select the node. For several nodes, create a group and choose **Load balancing** (round-robin) or **Random**, then select that group for the port.

For example, if you create an HTTP port on `127.0.0.1:8080`, an app on the same machine can use it like this:

```bash
curl --proxy http://127.0.0.1:8080 https://example.com
```

For a SOCKS5 port at the same address, use `socks5h://127.0.0.1:8080` instead.

## Screenshots

**Local Ports**

<img src="media/readme/proxyhub-local-ports-en.png" alt="Local ports" width="860">

<table>
  <tr>
    <th width="33%">Add Node</th>
    <th width="33%">Chain Node</th>
    <th width="33%">Batch Import</th>
  </tr>
  <tr>
    <td><a href="media/readme/proxyhub-add-node-en.png"><img src="media/readme/proxyhub-add-node-en.png" alt="Add node" width="280"></a></td>
    <td><a href="media/readme/proxyhub-chain-node-en.png"><img src="media/readme/proxyhub-chain-node-en.png" alt="Add chain node" width="280"></a></td>
    <td><a href="media/readme/proxyhub-batch-import-en.png"><img src="media/readme/proxyhub-batch-import-en.png" alt="Batch import nodes" width="280"></a></td>
  </tr>
</table>

Click a thumbnail to view the full image.

## Configuration

ProxyHub reads runtime settings from the active data directory:

- npm global install: `~/.proxy-hub/config.yaml`
- source/local binary direct run: `./data/config.yaml`

Common keys:

| Key | Purpose |
| --- | --- |
| `serveAt` | Service listen address, default `:3020`. |
| `dbUrl` | Database DSN, default `data.db` under the active data directory. |
| `logLevel` | Service log level. |

Only SQLite DSNs are supported.

## Disclaimer

This project was developed for learning purposes and is only used by the author for mutual access between two machines in the living room and bedroom at home. It is open sourced to comply with GPL requirements. Users are responsible for any consequences of their own use.

## Acknowledgements

- [sing-box](https://github.com/SagerNet/sing-box): provides the core functionality.
- [easy_proxies](https://github.com/jasonwong1991/easy_proxies): inspired the node blocklist and other implementation ideas.
- [Linux.do community](https://linux.do/): provides an open source exchange platform.

## License

ProxyHub is distributed under GPL-3.0-or-later because the released application links against SagerNet sing/sing-box.
