# Security Policy

The **GonDesk** team and community take security seriously. Because GonDesk provides remote desktop access and global NAT traversal (HTTPS/WSS + STUN/TURN), protecting peer sessions, credentials, and transport channels is a top priority.

---

## 🛡️ Supported Versions

The following versions of GonDesk are currently supported with security updates:

| Version | Supported          |
| ------- | ------------------ |
| `0.1.x` (Latest `master` release) | ✅ Supported |
| `< 0.1.0` | ❌ Not Supported |

---

## 🔒 Built-In Security Architecture

1. **TLS 1.3 Encryption on Single Port**:
   - All HTTP REST calls (`https://`), WebSocket signaling (`wss://`), and TCP TURN relays (`turns:`) multiplexed on the single port are encrypted using TLS 1.2 / TLS 1.3.
   - For production deployments of the **Global Bridge Server (`--mode=server`)**, we strongly recommend supplying a valid CA-signed certificate (e.g., Let's Encrypt) via `--cert` / `GONDESK_CERT_FILE` and `--key` / `GONDESK_KEY_FILE` instead of relying on the auto-generated self-signed certificate.
2. **Interactive Approval vs. Unattended Access**:
   - In **Desktop Mode**, incoming remote sessions require explicit interactive user approval via the **Incoming Connection Request** modal unless **Unattended Access** is explicitly enabled with a non-empty password.
3. **Server Mode Isolation (`--mode=server`)**:
   - Running GonDesk with `--mode=server` (or `GONDESK_MODE=server`) disables and hides all Desktop remote-control host/client handlers (`ShowDesktopFeatures() == false`), ensuring a Bridge Server node cannot be remotely controlled as a desktop workstation.
4. **TURN Authentication**:
   - Pion TURN allocations require long-term credential authentication (`HMAC-SHA1` / `stun.MessageIntegrity`) verified against the node's SQLite credential store.

---

## 🚨 Reporting a Vulnerability

**Please DO NOT report security vulnerabilities through public GitHub issues, discussions, or pull requests.**

If you discover a security vulnerability in GonDesk, please report it privately using one of the following channels:

1. **GitHub Private Vulnerability Reporting (Preferred)**:
   - Navigate to the **Security** tab of this GitHub repository and click **"Report a vulnerability"** to open a private security advisory.
2. **Direct Maintainer Contact**:
   - If private vulnerability reporting is not enabled on your fork/mirror, please contact the repository owner directly via their GitHub profile contact address with `[SECURITY] GonDesk Vulnerability Report` in the subject line.

### What to Include in Your Report
- Type of issue (e.g., authentication bypass, TLS multiplexer flaw, TURN relay abuse, SQL injection, remote code execution).
- Full paths of the source file(s) related to the manifestation of the issue.
- Step-by-step instructions or proof-of-concept (PoC) code to reproduce the issue.
- Impact of the vulnerability and any suggested mitigation.

### Response Timeline
- **Acknowledgement**: Within **48 hours** of receiving your report.
- **Triage & Assessment**: Within **5 business days**.
- **Fix & Disclosure**: We aim to release a patched version within **14–30 days** depending on complexity, and will credit you in the security advisory and `CHANGELOG.md` (unless you prefer to remain anonymous).

