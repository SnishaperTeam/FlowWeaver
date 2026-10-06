# FlowWeaver

[中文](README.md) | [English](README_EN.md) | [Русский](README_RU.md)

**FlowWeaver** 是一款功能全面的本地代理客户端与网关工具，围绕 **Clash 订阅**、**全协议节点**、**TUN 虚拟网卡** 和 **WireGuard 隧道** 构建，面向需要在复杂网络环境下获得稳定、可控访问体验的完整方案。

[![Go Version](https://img.shields.io/badge/Go最低版本-1.27%2B-00ADD8?style=flat&logo=go)](https://golang.org) [![License](https://img.shields.io/badge/许可证-AGPL--3.0-blue?style=flat&logo=open-source-initiative)](LICENSE) [![GitHub last commit](https://img.shields.io/github/last-commit/SnishaperTeam/FlowWeaver?style=flat&logo=git&label=最后提交)](https://github.com/SnishaperTeam/FlowWeaver/commits/main) [![GitHub Release](https://img.shields.io/github/v/release/SnishaperTeam/FlowWeaver?style=flat&logo=github&label=版本)](https://github.com/SnishaperTeam/FlowWeaver/releases)

本项目提供跨平台支持。详见 **[Platform.md](Platform.md)**。

---

## 与 SniShaper 的关系

本项目源自 [SniShaper](https://github.com/SnishaperTeam/SniShaper)，由原作者主导开发，两者**并行维护**，分工不同：

| | [SniShaper](https://github.com/SnishaperTeam/SniShaper) | FlowWeaver |
|---|---|---|
| 定位 | 轻量级代理服务端 | 功能全面的客户端 / 网关 |
| 核心能力 | ECH 注入、TLS 分片、会话迁移 | Clash 订阅适配、全协议节点、WireGuard、TUN 网关 |
| 依赖 | 尽量少，便于服务端部署 | 功能优先，接受更多依赖 |
| 维护 | 持续 | 持续 |

两者共享部分技术思路（sing-tun 数据面、uTLS/ECH 处理），但**代码库独立**，可各自演进。订阅与规则文件格式兼容，可以互相导入。

---

## 特性

### 订阅与节点

- **Clash 订阅导入**：直接导入机场的 Clash 订阅链接，自动解析节点、代理组与分流规则。
- **全协议节点**：ss（AEAD-2022）、vmess、vless（TCP / WebSocket）、trojan、tuic、hysteria2、wireguard、socks5、http。
- **UDP 转发**：除 http 外全部协议均支持 UDP，包括 QUIC / HTTP3。
- **WireGuard 隧道**：以用户态 IP 栈接入 WireGuard 节点，不创建内核网卡、不修改系统路由表。
- **规则来源可选**：内置规则不提供节点选择；订阅启用时加载其规则并隐藏内置规则。
- **节点选择页**：对齐 Clash Verge 的节点列表布局、选中态与切换体验。

### 代理与网关

- **TUN 虚拟网卡**：全局流量透明劫持，自动路由与 DNS 劫持。
- **多种代理模式**：MITM、Transparent、TLS-RF、QUIC、Migration、Direct。
- **智能分流**：基于 GFWList 自动识别受限域名。
- **加密 DNS**：内置抗污染解析，支持多节点故障转移。
- **Cloudflare IP 优选池**：自动测速、健康检查与刷新。
- **NAT64**：在 IP 受限环境下提供 IPv6 出口。

### 命令行

- **完整 CLI**：`snishaper` 命令覆盖订阅、代理、TUN、证书、规则与更新。
- **交互式 TUI**：终端内完成全部常用操作。

---

## 快速开始

本项目提供 GUI 与 CLI 两种形态。

```bash
# 导入 Clash 订阅
snishaper sub add https://example.com/subscribe MyProvider

# 启用订阅并选择节点
snishaper sub use MyProvider
snishaper sub nodes
snishaper sub select Proxy "HK-01"

# 启动代理
snishaper proxy on
```

GUI 用户请从 [Releases](https://github.com/SnishaperTeam/FlowWeaver/releases) 下载对应平台的稳定版本。

各平台的构建方式详见 [Platform.md](Platform.md)。

---

## 文档

技术原理与自定义规则指南请参阅 [**GitHub Wiki**](https://github.com/SnishaperTeam/FlowWeaver/wiki)。

---

## 贡献

欢迎提交 Issue 与 Pull Request。贡献前请阅读 [CONTRIBUTING.md](CONTRIBUTING.md) 与 [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md)。

---

## 许可

[GNU Affero General Public License v3.0](LICENSE)（AGPL-3.0）

本项目使用了以下开源组件，其许可协议同样适用：

- [sing-tun](https://github.com/Sagernet/sing-tun) — TUN 数据面
- [quic-go](https://github.com/quic-go/quic-go) — QUIC / HTTP3
- [wireguard-go](https://github.com/WireGuard/wireguard-go) — WireGuard 协议