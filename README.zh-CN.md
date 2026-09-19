<div align="center">
  <h1>ProxyHub</h1>

  <img src="media/readme/proxyhub-pool-flow-zh.png" alt="ProxyHub 代理格式转换" width="680">

  <p><strong>把手中的节点，变成应用能用的 HTTP/SOCKS5 代理。</strong></p>
  <p>VLESS、Hysteria2，甚至一台 SSH 服务器，都能通过本地代理端口接入。</p>

  <p>
    <a href="README.md">English</a> ·
    <a href="README.zh-CN.md">简体中文</a> ·
    <a href="#快速开始">安装</a> ·
    <a href="#界面截图">查看截图</a>
  </p>
</div>

## 解决什么问题

服务器上的应用只支持 HTTP/SOCKS 代理，但你手里只有几条 VLESS、Hysteria2（HY2），甚至只有一个 SSH 账号？ProxyHub 可以把它们转换成本地 HTTP/SOCKS5 代理，让应用直接使用。

可同时开启多个本地代理端口，每个端口对应一条或多条代理节点。基于 sing-box 开发。

## 特性

| 能力 | 说明 |
| --- | --- |
| 格式转换 | VLESS、VMess、Trojan、Shadowsocks、Hysteria/Hysteria2、TUIC、SSH、SOCKS5、HTTP → 本地 HTTP/SOCKS5 代理。 |
| 负载均衡 / 随机 | 负载均衡按顺序轮流分配新连接；随机模式为每个新连接随机选择节点。 |
| 节点选择 | 自动优选低延迟节点，也可手动指定；通过健康检查和故障转移应对不可用节点。 |
| 节点串联 | 让流量按顺序经过多个节点。 |
| 批量导入 | 粘贴代理链接或订阅，预览后导入。 |
| 备份迁移 | 用一份 JSON 导出、恢复配置。 |

## 快速开始

### npm

```bash
npm install -g pxhub
pxhub
```

打开 [http://127.0.0.1:3020](http://127.0.0.1:3020)。也可以使用兼容命令 `proxy-hub` 启动。

更新到最新版：`npm install -g pxhub@latest`。

### Docker

```bash
docker run -d --name proxyhub -p 3020:3020 -v proxyhub-data:/app/data ghcr.io/fy0/proxy-hub:latest
```

打开 [http://127.0.0.1:3020](http://127.0.0.1:3020)。若要从容器外使用代理端口，还需映射对应端口（如 `-p 8080:8080`），并在 ProxyHub 中将该代理端口的监听地址设为 `0.0.0.0`。

### 二进制

从 [GitHub Releases](https://github.com/fy0/proxy-hub/releases) 下载最新压缩包，解压后运行 `proxy-hub` 或 `proxy-hub.exe`。

### 使用示例：从节点到应用

在网页中添加节点，或将链接粘贴到批量导入。把下面的地址和凭据替换成你自己的；用户名、密码中的特殊字符需要做 URL 编码。

**SSH**：有服务器地址、用户名和密码即可填写：

```text
ssh://alice:your-password@ssh.example.com:22#My-SSH
```

**Hysteria2（HY2）**：填入节点密码和 TLS 服务器名称：

```text
hy2://your-password@hy2.example.com:443?sni=hy2.example.com#My-HY2
```

**VLESS**：下面是普通 TLS 节点示例；Reality 或其他传输方式请使用服务商提供的完整链接：

```text
vless://00000000-0000-4000-8000-000000000001@vless.example.com:443?security=tls&sni=vless.example.com&type=tcp#My-VLESS
```

创建一个本地 HTTP 或 SOCKS5 端口，选中节点即可使用。多条节点可以先建成代理组，选择**负载均衡**（轮询）或**随机轮换**，再让本地端口使用该代理组。

例如，创建监听 `127.0.0.1:8080` 的 HTTP 端口后，同一台机器上的应用就可以这样接入：

```bash
curl --proxy http://127.0.0.1:8080 https://example.com
```

如果创建的是同地址的 SOCKS5 端口，将代理地址改为 `socks5h://127.0.0.1:8080` 即可。

## 界面截图

**本地端口**

<img src="media/readme/proxyhub-local-ports-zh.png" alt="本地端口" width="860">

<table>
  <tr>
    <th width="33%">添加节点</th>
    <th width="33%">串联节点</th>
    <th width="33%">批量导入</th>
  </tr>
  <tr>
    <td><a href="media/readme/proxyhub-add-node-zh.png"><img src="media/readme/proxyhub-add-node-zh.png" alt="添加节点" width="280"></a></td>
    <td><a href="media/readme/proxyhub-chain-node-zh.png"><img src="media/readme/proxyhub-chain-node-zh.png" alt="添加串联节点" width="280"></a></td>
    <td><a href="media/readme/proxyhub-batch-import-zh.png"><img src="media/readme/proxyhub-batch-import-zh.png" alt="批量导入节点" width="280"></a></td>
  </tr>
</table>

点击小图可查看原图。

## 配置

ProxyHub 从当前数据目录读取运行配置：

- npm 全局安装：`~/.proxy-hub/config.yaml`
- 源码/本地二进制直接运行：`./data/config.yaml`

常用配置：

| 配置项 | 用途 |
| --- | --- |
| `serveAt` | 服务监听地址，默认 `:3020`。 |
| `dbUrl` | 数据库 DSN，默认位于当前数据目录下的 `data.db`。 |
| `logLevel` | 服务日志级别。 |

仅支持 SQLite DSN。

## 声明

本项目出于学习目的开发，仅用于作者家里客厅和卧室两台机器的互相访问。因 GPL 协议要求开源。使用者需要自行承担使用后果。

## 鸣谢

- [sing-box](https://github.com/SagerNet/sing-box)：提供核心功能。
- [easy_proxies](https://github.com/jasonwong1991/easy_proxies)：借鉴了节点拉黑等实现思路。
- [Linux.do 社区](https://linux.do/)：提供开源交流平台。

## 许可证

ProxyHub 按 GPL-3.0-or-later 分发，因为发布产物链接了 SagerNet sing/sing-box。
