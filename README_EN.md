# FlowWeaver

[中文](README.md) | [English](README_EN.md) | [Русский](README_RU.md)

[![Go Version](https://img.shields.io/badge/Go-1.27%2B-00ADD8?style=flat&logo=go)](https://golang.org) [![License](https://img.shields.io/badge/License-AGPL--3.0-blue?style=flat&logo=open-source-initiative)](LICENSE) [![Wiki](https://img.shields.io/badge/Docs-Wiki-orange?style=flat&logo=readthedocs)](https://github.com/SnishaperTeam/FlowWeaver/wiki) [![GitHub Release](https://img.shields.io/github/v/release/SnishaperTeam/FlowWeaver?style=flat&logo=github&label=Release)](https://github.com/SnishaperTeam/FlowWeaver/releases) [![GitHub Downloads](https://img.shields.io/github/downloads/SnishaperTeam/FlowWeaver/total?style=flat&logo=github&label=Downloads)](https://github.com/SnishaperTeam/FlowWeaver/releases) [![GitHub last commit](https://img.shields.io/github/last-commit/SnishaperTeam/FlowWeaver?style=flat&logo=git&label=Last%20Commit)](https://github.com/SnishaperTeam/FlowWeaver/commits/main) [![GitHub Actions Workflow Status](https://img.shields.io/github/actions/workflow/status/SnishaperTeam/FlowWeaver/build.yml?style=flat&logo=githubactions&label=CI)](https://github.com/SnishaperTeam/FlowWeaver/actions)

**FlowWeaver** is a full-featured local proxy client and gateway built around **Clash subscriptions**, **multi-protocol nodes**, the **TUN virtual NIC** and **WireGuard tunnels**, for users who need stable and controllable access in complex network environments.

This project provides cross-platform support. See **[Platform_EN.md](Platform_EN.md)**.

---

## Relationship to SniShaper

This project grew out of [SniShaper](https://github.com/SnishaperTeam/FlowWeaver) and is led by the same maintainer. Both are **actively maintained** with different scopes:

| | [SniShaper](https://github.com/SnishaperTeam/FlowWeaver) | FlowWeaver |
|---|---|---|
| Focus | Lightweight proxy server | Full-featured client / gateway |
| Core strengths | ECH injection, TLS fragmentation, session migration | Clash subscriptions, multi-protocol nodes, WireGuard, TUN gateway |
| Dependencies | Kept minimal for server deployment | Feature-first, accepts more dependencies |
| Maintenance | Ongoing | Ongoing |

They share some technical ideas (the sing-tun data plane, uTLS/ECH handling) but keep **separate codebases** and evolve independently. Subscription and rule files are format-compatible and can be imported into either.

---

## Relationship to SniShaper

This project grew out of [SniShaper](https://github.com/SnishaperTeam/SniShaper) and is led by the same maintainer. Both are **actively maintained** with different scopes:

| | [SniShaper](https://github.com/SnishaperTeam/SniShaper) | FlowWeaver |
|---|---|---|
| Focus | Lightweight proxy server | Full-featured client / gateway |
| Core strengths | ECH injection, TLS fragmentation, session migration | Clash subscriptions, multi-protocol nodes, WireGuard, TUN gateway |
| Dependencies | Kept minimal for server deployment | Feature-first, accepts more dependencies |
| Maintenance | Ongoing | Ongoing |

They share some technical ideas (the sing-tun data plane, uTLS/ECH handling) but keep **separate codebases** and evolve independently. Subscription and rule files are format-compatible and can be imported into either.

---

## Features

- **Multi-Mode Proxy**: MITM (man-in-the-middle), Transparent, TLS-RF (TLS fragmentation), QUIC, Migration (session migration), Direct — covering a wide range of site scenarios.
- **TUN Virtual NIC**: Transparent global traffic hijacking, auto-routing and DNS hijacking.
- **ECH Injection**: Automatically fetches and injects ECH Config, with DoH discovery and hot-reload.
- **Smart Routing**: Auto-identifies blocked domains based on GFWList, automatically covering many sites not included in the rules.
- **Encrypted DNS**: Built-in anti-pollution DNS resolver with multi-node failover for stable resolution.
- **Cloudflare IP Pool**: Auto speed-test, health check, and refresh.
- **NAT64 Support**: More flexible IP egress, enabling service access under IP blocking.
- **Evolution Mode**: Automatically tests combinations of rules to find the optimal access method for a target site and applies it with one click.

---

## Quick Start

The project started out Windows-only and later added Linux support. It now ships both a GUI and a CLI.

For most users, we recommend downloading the latest stable Windows build straight from [Releases](https://github.com/SnishaperTeam/FlowWeaver/releases).

For the other platforms and for build instructions, see the following documents:

- **[Platform_EN.md](Platform_EN.md)** — per-platform quick start, CLI usage and mobile companions.
- **[build_EN.md](build_EN.md)** — build guide and the 12-target artifact matrix.

---

## Documentation

For detailed technical principles and custom rule guides, refer to the [**GitHub Wiki**](https://github.com/SnishaperTeam/FlowWeaver/wiki):

- **[Core Mode Introduction](https://github.com/SnishaperTeam/FlowWeaver/wiki/Core-Proxy-Modes)**: Understand TLS-RF, QUIC and Server mode operation.
- **[Rule Customization Guide](https://github.com/SnishaperTeam/FlowWeaver/wiki/Custom-Rules-Guide)**: Learn how to develop targeted rules.
- **[GUI Configuration Practice](https://github.com/SnishaperTeam/FlowWeaver/wiki/GUI-Configuration)**: Quickly configure rules in the GUI.
- **[FAQ](https://github.com/SnishaperTeam/FlowWeaver/wiki/FAQ)**: Resolve certificate warnings, rule issues and other common problems.

---

## Build and Development

The frontend is currently built with **Wails v3 + React 19 + MUI**, with the core developed in **Go**, supporting Windows / Linux dual-platform GUI and cross-platform CLI for a total of 12 build targets. The full build guide is in **[build_EN.md](build_EN.md)**.

We will complete a native GUI implementation in an upcoming stable release, to reduce the frontend's memory footprint.

---

## Acknowledgements

This project has benefited from the inspiration of the following excellent open-source projects:

- [DoH-ECH-Demo](https://github.com/0xCaner/DoH-ECH-Demo)
- [lumine](https://github.com/moi-si/lumine)

## Project Activity & Contributors

### Activity Badges

[![GitHub contributors](https://img.shields.io/github/contributors/SnishaperTeam/FlowWeaver?style=flat&label=Total Contributors)](https://github.com/SnishaperTeam/FlowWeaver/graphs/contributors)
[![GitHub commit activity](https://img.shields.io/github/commit-activity/m/SnishaperTeam/FlowWeaver?style=flat&label=Monthly Commits)](https://github.com/SnishaperTeam/FlowWeaver/graphs/contributors)
[![GitHub last commit](https://img.shields.io/github/last-commit/SnishaperTeam/FlowWeaver?style=flat&label=Last Commit)](https://github.com/SnishaperTeam/FlowWeaver/commits/main)

### Activity Trend

<div align="center">
<a href="https://repobeats.axiom.co/" target="_blank">
<img src="https://repobeats.axiom.co/api/embed/f62c98a5231da45588ee71f26e3c1cc3f64edb6b.svg" alt="Repobeats analytics" />
</a>
</div>

### Contributors Graph

<div align="center">
<a href="https://github.com/SnishaperTeam/FlowWeaver/graphs/contributors" target="_blank">
<img src="https://contrib.rocks/image?repo=SnishaperTeam/FlowWeaver" alt="Contributors" />
</a>
</div>

## Star History

<a href="https://www.star-history.com/?repos=SnishaperTeam%2FFlowWeaver&type=timeline&logscale=&releases=&legend=bottom-right">
<picture>
<source media="(prefers-color-scheme: dark)" srcset="https://api.star-history.com/chart?repos=SnishaperTeam/FlowWeaver&type=timeline&theme=dark&logscale&legend=bottom-right" />
<source media="(prefers-color-scheme: light)" srcset="https://api.star-history.com/chart?repos=SnishaperTeam/FlowWeaver&type=timeline&logscale&legend=bottom-right" />
<img alt="Star History Chart" src="https://api.star-history.com/chart?repos=SnishaperTeam/FlowWeaver&type=timeline&logscale&legend=bottom-right" />
</picture>
</a>

---

## License

[GNU Affero General Public License v3.0](LICENSE) (AGPL-3.0).
