# Contributing to `wtop`

Thank you for your interest in contributing to `wtop`! We welcome bug reports, feature suggestions, documentation improvements, and code contributions.

Please review this guide before opening an issue or pull request.

---

## Code of Conduct & Privacy

* All contributors and participants are expected to uphold our [Code of Conduct](CODE_OF_CONDUCT.md).
* Please review our [Privacy Policy](PRIVACY.md). When submitting bug reports or logs, avoid sharing sensitive credentials or personal information.

---

## Architecture Overview

`wtop` is structured in three strictly one-directional layers:

```text
cmd/wtop  ──►  internal/ui  ──►  internal/collector
```

1. **`internal/collector`**: Produces immutable `Snapshot` structs containing CPU, memory, GPU, network, and process metrics. Contains zero UI or styling logic.
2. **`internal/ui`**: Root Bubble Tea `Model` handling key navigation, state management, sorting, and layout arithmetic.
3. **`internal/ui/panels`**: Pure render functions (CPU gauges, memory/GPU bars, process table) taking metrics and viewport dimensions and returning Lipgloss-styled strings.

---

## Development Setup

### Prerequisites
* **Go 1.27+**
* **Windows 10 / 11** (AMD64 or ARM64)
* **PowerShell 7+** (Recommended)

### Building from Source

```powershell
# 1. Clone repository
git clone https://github.com/michaelsanford/wtop.git
cd wtop

# 2. Generate Windows resources (manifest, icon, versioninfo)
go generate ./cmd/wtop/

# 3. Build executable
go build -o wtop.exe ./cmd/wtop/

# 4. Run wtop
.\wtop.exe
```

---

## Testing & Quality Verification

Always verify that the test and benchmark suites pass before submitting a pull request:

```powershell
# Run unit tests
go test ./...

# Run deterministic benchmarks
go test -run='^$' -bench='^Benchmark[^L]' -benchmem ./...

# Run all benchmarks (including live syscalls)
go test -run='^$' -bench='.' -benchmem ./...

# Verify code formatting and vetting
go vet ./...
$env:GOOS="linux"; go vet ./...   # Verify cross-platform stubs
$env:GOOS="windows"

# Run linter (golangci-lint v2)
golangci-lint run
```

### The Go and golangci-lint pins move together

Every workflow resolves its toolchain with `go-version-file: go.mod`, so the `go` directive in
`go.mod` **is** the CI Go version — raising it raises CI's toolchain too.

This matters because **golangci-lint fails open when it is older than the Go it is linting**. A
linter built against an older toolchain cannot typecheck the newer standard library: it writes
`typechecking error: ... could not import math/rand/v2` to stderr and then reports `0 issues`. That
reads as a clean run while nothing has actually been linted, and it is how a `gosec` G304 finding
once reached CI after a local run reported no problems.

So when you change the `go` directive in `go.mod`, in the same commit:

1. Bump the `golangci-lint` pin in `.github/workflows/ci.yml` to a release built against that Go.
2. Update the three places that state the minimum version: the badge and the "Requires Go" line in
   `README.md`, and the **Prerequisites** section above.
3. Prove the linter still lints, rather than trusting `0 issues`. Drop a file with a known violation
   into a package, confirm it is reported, then delete it:

   ```go
   package somepkg

   import "os"

   func canary(p string) []byte { b, _ := os.ReadFile(p); return b } // must trip gosec G304
   ```

   An exit code of `0` accompanied by typechecking errors on stderr means the run was worthless.

Current pairing: **Go 1.27.0** with **golangci-lint v2.13.2** (v2.12.2 cannot typecheck a Go 1.27
standard library).

---

## Git Conventions

### Branch Naming (Conventional Branch 1.1.0)
Always branch from `main` using structured prefixes:
* `feat/<description>` — New features
* `fix/<description>` — Bug fixes
* `docs/<description>` — Documentation updates
* `chore/<description>` — Maintenance, dependencies, tooling

*Example*: `feat/network-panel-expansion`

### Commit Messages (Conventional Commits v1.0.0)
Format all commits using Conventional Commits:
```text
<type>(<scope>): <short description in imperative mood>

[optional body describing rationale]
```

*Types*: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`, `build`, `ci`.

*Example*:
```text
feat(collector): add per-process thread count metric (#42)

Collects active thread count per process via Win32 process counters.
```

---

## Submitting Pull Requests

1. Open a pull request against `main`.
2. Complete the checklist in the pull request template.
3. Ensure all CI checks (`Build & Test`, `CodeQL`) pass cleanly.
4. Maintainers will review your PR and provide constructive feedback.

---

## Reporting Issues

* **Bug Reports**: Open an issue using the [Bug Report template](https://github.com/michaelsanford/wtop/issues/new?template=bug_report.yml). If reporting a false-positive detection by antivirus software, please include your antivirus signature/definition version.
* **Feature Requests**: Open an issue using the [Feature Request template](https://github.com/michaelsanford/wtop/issues/new?template=feature_request.yml) describing the proposed feature, panel, or keyboard shortcut.
