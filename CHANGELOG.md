# Changelog

All notable changes to **GonDesk** will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-11-14

### Added
- **MyGo Native UI (`github.com/egoist/mygo/ui`)**:
  - Cross-platform GPU-accelerated Native UI (`CGO_ENABLED=0`) with AnyDesk-inspired dark theme.
  - **Desktop Mode (`--mode=desktop`)**: "This Desk" panel (9-digit GonDesk ID, copy ID, unattended access password, incoming connection approval modal) and "Remote Desk" panel (target ID/URL input, password, active remote session viewport, remote input actions, and live session chat).
  - **Desktop Server Mode (`--mode=server`)**: Single-codebase Global Bridge & TURN Server mode with all Desktop remote/be-remoted UI features hidden, plus `--headless` support for VPS and Docker deployments.
- **Single-Port Multiplexer (`internal/netmux`)**:
  - Multiplexes **HTTPS REST API**, **WSS WebSocket Signaling**, **TURNS (TLS/TCP)**, and **TURN/STUN (UDP)** over a single port (`:8443` by default).
  - Automatic self-signed ECDSA P-256 X.509 certificate generation (`certs/server.crt` & `certs/server.key`).
- **GoFiber v2 & Pion TURN v4 Integration**:
  - REST API endpoints (`/api/v1/info`, `/api/v1/peers`, `/api/v1/peers/heartbeat`, `/api/v1/sessions`, `/api/v1/turn/credentials`) and `/ws/signal` WebSocket signaling hub.
  - Embedded Pion TURN v4 server with SQLite-backed credential lookup.
- **Pure-Go SQLite Persistence (`internal/database`)**:
  - GORM integration with `github.com/glebarez/sqlite` (`CGO_ENABLED=0`) for `AppSetting`, `PeerRecord`, `SessionLog`, and `TurnCredential`.
- **Docker & CI/CD Automation**:
  - Multi-stage `Dockerfile` and `docker-compose.yml` for 1-port Global Bridge deployment.
  - GitHub Actions workflow (`.github/workflows/release.yml`) for automatic semantic versioning (`vX.Y.Z`), multi-arch Docker Hub push, and 3-OS release asset packaging (Linux, Windows, macOS).

