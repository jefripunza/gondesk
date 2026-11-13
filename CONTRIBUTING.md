# Contributing to GonDesk

Thank you for your interest in contributing to **GonDesk**! Whether you are fixing a bug, proposing a new feature, improving documentation, or testing across platforms, your contributions are warmly welcomed.

By participating in this project, you agree to abide by our [Code of Conduct](CODE_OF_CONDUCT.md).

---

## 📋 Table of Contents

1. [Ways to Contribute](#-ways-to-contribute)
2. [Development Environment Setup](#️-development-environment-setup)
3. [Architecture & MyGo Native UI Rules](#-architecture--mygo-native-ui-rules)
4. [Testing & Static Analysis](#-testing--static-analysis)
5. [Commit Message & Auto-Versioning Convention](#-commit-message--auto-versioning-convention)
6. [Pull Request Process](#-pull-request-process)

---

## 💡 Ways to Contribute

- **Report Bugs**: Open an issue using the [Bug Report template](.github/ISSUE_TEMPLATE/bug_report.md) with reproduction steps, OS details, and logs.
- **Suggest Enhancements**: Open an issue using the [Feature Request template](.github/ISSUE_TEMPLATE/feature_request.md).
- **Improve Documentation**: Fix typos, add deployment guides, or translate docs.
- **Write Code**: Pick up an open issue, improve the single-port multiplexer, enhance Pion TURN relay handling, or polish the MyGo Native UI.

---

## 🛠️ Development Environment Setup

### Prerequisites
- **Go `1.27.3` or newer** (or Go `1.24+` with `GOTOOLCHAIN=auto` enabled, which automatically downloads the required Go toolchain pinned in `go.mod`).
- **OS Requirements**:
  - **macOS 12+**: No extra packages needed (uses Metal natively).
  - **Windows 10/11**: No extra packages needed (uses Direct3D 11 natively).
  - **Linux**: Install GTK 3 runtime (`sudo apt-get install libgtk-3-0` on Ubuntu/Debian). No WebKitGTK is required because GonDesk uses pure Native UI (`linux.webview: false`).

### Clone & Run Locally

```bash
git clone https://github.com/<your-username>/gondesk.git
cd gondesk

# Run in Desktop Mode with hot-reload
go tool mygo dev

# Run in Global Bridge Server Mode (Desktop features hidden)
GONDESK_MODE=server go tool mygo dev
```

---

## 🧭 Architecture & MyGo Native UI Rules

GonDesk is built on a **Single Codebase, Dual Mode, Single Port** architecture:

1. **Dual Runtime Modes (`internal/config`, `internal/uiapp`)**:
   - Any feature specific to interactive desktop remote control (*Me-remote* / *Di-remote*) **must** respect `cfg.ShowDesktopFeatures()` so that `--mode=server` cleanly hides desktop client/host features.
2. **Single-Port Multiplexing (`internal/netmux`, `internal/server`, `internal/turnserver`)**:
   - Do not bind extra ports for signaling, HTTP, or TURN. All protocols (HTTPS, WSS, TURNS, STUN/TURN UDP) must share the single configured port (`cfg.ListenPort`).
3. **Pure Go (`CGO_ENABLED=0`)**:
   - Do not introduce `cgo` dependencies. This ensures instant cross-compilation across macOS, Windows, and Linux.
4. **MyGo Native UI (`github.com/egoist/mygo/ui`) Guidelines**:
   - **Never store `ui.Element` or `*ui.Context`** in struct fields, globals, or goroutines. Elements live only for a single frame build pass.
   - **Key dynamic rows in loops**: Always pass `c.Key(id)` when rendering interactive widgets inside `for` loops.
   - **Thread safety**: Mutate UI state from background goroutines (e.g., WebSocket or TURN callbacks) using `win.Update(func() { ... })`.

---

## 🧪 Testing & Static Analysis

Before submitting a Pull Request, ensure all static checks and tests pass:

```bash
# 1. Run MyGo static analyzer (verifies UI element lifetimes & goroutine rules)
go tool mygo vet .

# 2. Run standard Go vet
go vet ./...

# 3. Run headless UI unit tests & single-port HTTPS/TURN/WSS integration tests
go test -v ./...

# 4. Verify compilation across packages
go build -v ./...
```

When adding new UI views or network features, please add corresponding tests in `main_test.go` (using `ui.NewTester` for headless Native UI assertions).

---

## 🏷️ Commit Message & Auto-Versioning Convention

Pushes/merges to `master` automatically calculate the next semantic version (`vX.Y.Z`), tag the release, push to Docker Hub, and build binaries for Linux, Windows, and macOS.

Please follow [Conventional Commits](https://www.conventionalcommits.org/):

- **Patch Release (`0.1.X`)** — Bug fixes, documentation, refactoring, chores:
  - `fix(netmux): handle TLS alert gracefully on single port`
  - `docs(readme): add docker compose example`
- **Minor Release (`0.X.0`)** — New backward-compatible features (`feat:` or `#minor`):
  - `feat(ui): add session latency graph in active viewport`
- **Major Release (`X.0.0`)** — Breaking changes (`feat!:` or `BREAKING CHANGE` or `#major`):
  - `feat!(api): change heartbeat payload schema #major`

---

## 🔄 Pull Request Process

1. **Fork** the repository and create your branch from `master`:
   ```bash
   git checkout -b feat/your-feature-name
   ```
2. **Make your changes** and format your code with `gofmt -s -w .`.
3. **Run checks**: Ensure `go tool mygo vet .` and `go test -v ./...` pass with zero errors.
4. **Open a Pull Request** against the `master` branch and fill out the [Pull Request Template](.github/PULL_REQUEST_TEMPLATE.md).
5. A maintainer will review your PR as soon as possible. Thank you for contributing to GonDesk!

