# `wtop`

[![CI](https://github.com/michaelsanford/wtop/actions/workflows/ci.yml/badge.svg)](https://github.com/michaelsanford/wtop/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/michaelsanford/wtop)](https://github.com/michaelsanford/wtop/releases)
[![Windows](https://img.shields.io/badge/platform-Windows-0078d7?logo=windows&logoColor=white)](https://github.com/michaelsanford/wtop/releases)

[![Go](https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white)](go.mod)
[![SBOM](https://img.shields.io/badge/SBOM-CycloneDX-4caf50)](https://github.com/michaelsanford/wtop/releases)
[![Signed](https://img.shields.io/badge/signed-Sigstore-3f51b5)](https://github.com/michaelsanford/wtop/releases)
[![Attestation](https://img.shields.io/badge/attestation-GitHub-24292e?logo=github&logoColor=white)](https://github.com/michaelsanford/wtop/attestations)

[![License](https://img.shields.io/github/license/michaelsanford/wtop)](LICENSE)

A self-contained, single-binary terminal system monitor for Windows, inspired by htop and written in Go.

![wtop screenshot](docs/wtop.png)

## Features

- **CPU** — per-core utilisation bars with colour coding (green → yellow → red)
- **Memory** — RAM and swap bars in GiB
- **GPU** — best-effort: NVIDIA via `nvidia-smi`, AMD/Intel via PowerShell `Get-Counter`; loads in the background on startup so other panels appear immediately
- **Process list** — flat or htop-style tree view (`t`); sortable by CPU%, memory, PID, or name; kill selected process

## Keyboard shortcuts

| Key            | Action                                        |
|----------------|-----------------------------------------------|
| `q` / `Ctrl+C` | Quit                                          |
| `↑` / `↓`      | Scroll process list                           |
| `s`            | Cycle sort column (CPU% → MEM% → PID → Name)  |
| `d`            | Invert sort order                             |
| `t`            | Toggle tree view (htop-style parent → child)  |
| `x`            | Kill selected process (confirmation required) |
| `g`            | Cycle GPUs (if multiple)                      |

## Download

Grab the latest binary from the [Releases](../../releases) page. No installer needed — just run it.

```powershell
.\wtop.exe
```

## Build from source

Requires Go 1.26+.

```powershell
go build -o wtop.exe ./cmd/wtop/
```

## Supply chain security

Every release includes:

- **CycloneDX SBOM** (`wtop-vX.Y.Z-sbom.cdx.json`) — full dependency inventory
- **Cosign bundle** (`*.bundle`) — keyless signature via Sigstore
- **GitHub build provenance attestation** — verifiable via `gh attestation verify`

Verify a release binary:

```sh
gh attestation verify wtop-v0.1.0-windows-amd64.exe --repo michaelsanford/wtop
```

## License

MIT
