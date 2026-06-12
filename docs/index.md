---
layout: default
title: wtop — Windows system monitor
---

[![CI](https://github.com/michaelsanford/wtop/actions/workflows/ci.yml/badge.svg)](https://github.com/michaelsanford/wtop/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/michaelsanford/wtop)](https://github.com/michaelsanford/wtop/releases)
[![License](https://img.shields.io/github/license/michaelsanford/wtop)](https://github.com/michaelsanford/wtop/blob/main/LICENSE)
[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](https://github.com/michaelsanford/wtop/blob/main/go.mod)
[![Windows](https://img.shields.io/badge/platform-Windows-0078d7?logo=windows&logoColor=white)](https://github.com/michaelsanford/wtop/releases)
[![SBOM](https://img.shields.io/badge/SBOM-CycloneDX-4caf50)](https://github.com/michaelsanford/wtop/releases)
[![Signed](https://img.shields.io/badge/signed-Sigstore-3f51b5)](https://github.com/michaelsanford/wtop/releases)

**wtop** is a self-contained, single-binary system monitor for Windows — inspired by htop. No installer, no runtime dependencies, no telemetry.

![wtop screenshot](wtop.png)

---

## Features

| Panel | What you get |
|-------|-------------|
| **CPU** | Per-core utilisation bars with colour coding: green → yellow → red |
| **Memory** | RAM and swap bars in GiB with used / cached / buffer breakdown |
| **GPU** | Best-effort detection: NVIDIA via `nvidia-smi`, AMD/Intel via PowerShell `Get-Counter`, or N/A |
| **Network** | Per-interface send/receive rates in real time |
| **Process list** | Sortable by CPU%, memory, PID, or name; kill any selected process |

---

## Keyboard shortcuts

| Key | Action |
|-----|--------|
| `↑` / `↓` or `k` / `j` | Scroll process list |
| `s` | Cycle sort column (CPU% → Mem MB → PID → Name) |
| `d` | Reverse sort order |
| `g` | Cycle between GPUs (if multiple detected) |
| `x` | Kill selected process (asks for confirmation) |
| `y` / `n` / `Esc` | Confirm / cancel kill |
| `q` / `Ctrl+C` | Quit |

---

## Download

Grab the latest pre-built binary from the [**Releases**](https://github.com/michaelsanford/wtop/releases) page. No installer needed.

```powershell
# Run directly after download
.\wtop-v1.0.0-windows-amd64.exe
```

Binaries are available for **amd64** and **arm64**.

---

## Build from source

Requires Go 1.26+.

```powershell
git clone https://github.com/michaelsanford/wtop.git
cd wtop
go build -o wtop.exe ./cmd/wtop/
.\wtop.exe
```

---

## Supply chain security

Every release is built reproducibly in GitHub Actions and ships with:

- **CycloneDX SBOM** (`wtop-vX.Y.Z-sbom.cdx.json`) — full dependency inventory in JSON format
- **Cosign bundle** (`*.bundle`) — keyless signature via [Sigstore](https://sigstore.dev)
- **GitHub build provenance attestation** — verifiable with the `gh` CLI

### Verify a release binary

```sh
# Verify build provenance attestation
gh attestation verify wtop-v1.0.0-windows-amd64.exe --repo michaelsanford/wtop

# Verify Sigstore cosign signature
cosign verify-blob wtop-v1.0.0-windows-amd64.exe \
  --bundle wtop-v1.0.0-windows-amd64.exe.bundle \
  --certificate-identity-regexp "https://github.com/michaelsanford/wtop" \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

---

## License

[MIT](https://github.com/michaelsanford/wtop/blob/main/LICENSE) — Michael Sanford
