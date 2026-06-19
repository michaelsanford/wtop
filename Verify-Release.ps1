#!/usr/bin/env pwsh
#Requires -Version 7
<#
.SYNOPSIS
    Verify the latest (or specified) wtop release: GitHub attestation, cosign
    keyless signature, and SBOM integrity.

.PARAMETER Repo
    GitHub owner/repo slug. Default: michaelsanford/wtop

.PARAMETER Tag
    Release tag to verify. Default: latest published release.

.PARAMETER Force
    Re-download assets even if they already exist in downloads/.

.PARAMETER WinGet
    Instead of downloading the release, validate the binary that winget
    installed (michaelsanford.wtop). Verifies GitHub build provenance and the
    cosign keyless signature against the on-disk binary. The cosign .bundle is
    downloaded to a temp location and deleted afterwards. The SBOM is not
    checked in this mode because winget does not install it.

.PARAMETER Version
    Release version/tag used to select the cosign bundle in -WinGet mode.
    Accepts either "v1.1.0" or "1.1.0". When omitted, the version is
    auto-detected from `winget show`.

.EXAMPLE
    .\verify-release.ps1
    .\verify-release.ps1 -Tag v0.5.0
    .\verify-release.ps1 -WinGet
    .\verify-release.ps1 -WinGet -Version v1.1.0
#>
param(
    [string]$Repo    = "michaelsanford/wtop",
    [string]$Tag     = "",
    [switch]$Force,
    [switch]$WinGet,
    [string]$Version = ""
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# ── Helpers ───────────────────────────────────────────────────────────────────
function Write-Pass([string]$msg) {
    Write-Host "  [PASS] $msg" -ForegroundColor Green
}
function Write-Fail([string]$msg) {
    Write-Host "  [FAIL] $msg" -ForegroundColor Red
}
function Write-Info([string]$msg) {
    Write-Host "         $msg" -ForegroundColor Cyan
}
function Write-Warn([string]$msg) {
    Write-Host "  [WARN] $msg" -ForegroundColor Yellow
}
function Write-Header([string]$msg) {
    Write-Host ""
    Write-Host $msg -ForegroundColor White
    Write-Host ("─" * [Math]::Max($msg.Length, 60)) -ForegroundColor DarkGray
}

# ── Prerequisite check ────────────────────────────────────────────────────────
Write-Header "Checking prerequisites"

# Required tools: check all before exiting so the user sees every missing item.
$prereqsFailed = $false

if (Get-Command "gh" -ErrorAction SilentlyContinue) {
    Write-Pass "gh"
} else {
    Write-Fail "gh not found"
    Write-Info "Install: winget install GitHub.cli"
    $prereqsFailed = $true
}

# winget installs cosign as "cosign-windows-amd64" rather than "cosign".
$cosignCmd = @("cosign", "cosign-windows-amd64") |
    Where-Object { Get-Command $_ -ErrorAction SilentlyContinue } |
    Select-Object -First 1
if ($cosignCmd) {
    Write-Pass "cosign ($cosignCmd)"
} else {
    Write-Fail "cosign not found"
    Write-Info "Install: go install github.com/sigstore/cosign/v2/cmd/cosign@latest"
    Write-Info "     or: winget install sigstore.cosign"
    $prereqsFailed = $true
}

# winget is required only when validating a winget-installed binary.
if ($WinGet) {
    if (Get-Command "winget" -ErrorAction SilentlyContinue) {
        Write-Pass "winget"
    } else {
        Write-Fail "winget not found"
        Write-Info "winget ships with App Installer from the Microsoft Store"
        $prereqsFailed = $true
    }
}

# Optional: cyclonedx-cli enables formal JSON schema validation of the SBOM.
# It is irrelevant in -WinGet mode (the SBOM is not installed by winget).
$hasCycloneDX = $false
if (-not $WinGet) {
    $hasCycloneDX = [bool](Get-Command "cyclonedx" -ErrorAction SilentlyContinue)
    if ($hasCycloneDX) {
        Write-Pass "cyclonedx-cli (schema validation enabled)"
    } else {
        Write-Warn "cyclonedx-cli not found — skipping formal schema validation"
        Write-Info "Install: winget install CycloneDX.CLI"
    }
}

if ($prereqsFailed) {
    Write-Host ""
    Write-Host "  Install missing required tools and re-run." -ForegroundColor Red
    exit 1
}

# ═══════════════════════════════════════════════════════════════════════════════
# WinGet mode — validate the winget-installed binary instead of the release assets.
# Verifies build provenance + cosign signature against the on-disk binary, never
# the winget shim. SBOM is skipped (winget does not install it).
# ═══════════════════════════════════════════════════════════════════════════════
if ($WinGet) {
    $packageId = "michaelsanford.wtop"

    # Map a winget version (e.g. "1.1.0.0") or user input to a release tag ("v1.1.0").
    function ConvertTo-ReleaseTag([string]$v) {
        if (-not $v) { return $null }
        $v = $v.Trim()
        # winget appends a 4th component (1.1.0.0); the release tag is 3-part.
        if ($v -match '^(\d+\.\d+\.\d+)\.0$') { $v = $Matches[1] }
        if ($v -notmatch '^v') { $v = "v$v" }
        return $v
    }

    # ── Confirm the package is installed ──────────────────────────────────────
    Write-Header "Locating winget-installed $packageId"

    $listOut = & winget list --id $packageId --exact --disable-interactivity 2>&1
    if ($LASTEXITCODE -ne 0 -or ($listOut -join "`n") -notmatch [regex]::Escape($packageId)) {
        Write-Fail "$packageId is not installed via winget"
        Write-Info "Install: winget install $packageId"
        exit 1
    }
    Write-Pass "$packageId is installed"

    # Installed version, as `winget list` reports it (e.g. "1.1.0.0").
    $installedVersion = $null
    $listLine = $listOut | Where-Object { $_ -match [regex]::Escape($packageId) } | Select-Object -First 1
    if ($listLine -match '(\d+\.\d+\.\d+(?:\.\d+)?)') {
        $installedVersion = $Matches[1]
        Write-Info "Installed version (winget list): $installedVersion"
    }

    # ── Resolve the release tag used to fetch the cosign bundle ───────────────
    if ($Version) {
        $Tag = ConvertTo-ReleaseTag $Version
        Write-Info "Using -Version: $Tag"
    } else {
        # Auto-detect from `winget show`, which reports a clean 3-part version.
        $showOut = & winget show --id $packageId --exact --disable-interactivity 2>&1
        $showVersion = $null
        if ($LASTEXITCODE -eq 0) {
            $showLine = $showOut | Where-Object { $_ -match '^\s*Version:\s*(.+)$' } | Select-Object -First 1
            if ($showLine -match '^\s*Version:\s*(.+)$') { $showVersion = $Matches[1].Trim() }
        }
        $source = if ($showVersion) { $showVersion } else { $installedVersion }
        if (-not $source) {
            Write-Fail "Could not determine the installed version; pass -Version explicitly."
            exit 1
        }
        $Tag = ConvertTo-ReleaseTag $source
        Write-Info "Auto-detected version (winget show): $Tag"
    }

    # Warn if the catalog/requested tag differs from what is actually installed.
    if ($installedVersion) {
        $installedTag = ConvertTo-ReleaseTag $installedVersion
        if ($installedTag -ne $Tag) {
            Write-Warn "Resolved tag $Tag differs from installed $installedTag — pass -Version to override."
        }
    }

    # ── Locate the real binary (never the winget shim) ────────────────────────
    $linksDir     = Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Links"
    $packagesGlob = Join-Path $env:LOCALAPPDATA "Microsoft\WinGet\Packages\$packageId*\*.exe"

    $binaryPath = $null
    $cmd = Get-Command "wtop" -ErrorAction SilentlyContinue
    if ($cmd -and $cmd.Source) {
        $item = Get-Item -LiteralPath $cmd.Source -ErrorAction SilentlyContinue
        if ($item -and $item.LinkTarget) {
            # Resolve the shim's symlink to its real target.
            $target = $item.LinkTarget
            if (-not [System.IO.Path]::IsPathRooted($target)) {
                $target = Join-Path (Split-Path $item.FullName -Parent) $target
            }
            $resolved = Resolve-Path -LiteralPath $target -ErrorAction SilentlyContinue
            if ($resolved) { $binaryPath = $resolved.Path }
        } elseif ($item) {
            $binaryPath = $item.FullName
        }
    }

    # Reject the shim itself; fall back to scanning the Packages directory.
    if ($binaryPath -and ($binaryPath -like "$linksDir*")) {
        Write-Warn "Resolved path is the winget shim; scanning Packages directory instead."
        $binaryPath = $null
    }
    if (-not $binaryPath) {
        $candidate = Get-ChildItem -Path $packagesGlob -ErrorAction SilentlyContinue | Select-Object -First 1
        if ($candidate) { $binaryPath = $candidate.FullName }
    }

    if (-not $binaryPath -or -not (Test-Path -LiteralPath $binaryPath)) {
        Write-Fail "Could not locate the winget-installed wtop binary."
        Write-Info "Looked under: $packagesGlob"
        exit 1
    }
    if ($binaryPath -like "$linksDir*") {
        Write-Fail "Refusing to validate the winget shim: $binaryPath"
        exit 1
    }
    Write-Pass "Binary: $binaryPath"

    # ── Determine architecture for bundle selection ───────────────────────────
    switch ($env:PROCESSOR_ARCHITECTURE) {
        "AMD64" { $arch = "amd64" }
        "ARM64" { $arch = "arm64" }
        default {
            $arch = "amd64"
            Write-Warn "Unrecognized architecture '$env:PROCESSOR_ARCHITECTURE' — assuming amd64"
        }
    }
    $bundleName = "wtop-$Tag-windows-$arch.exe.bundle"
    Write-Info "Architecture: $arch  (bundle: $bundleName)"

    # ── Result tracking ───────────────────────────────────────────────────────
    $attestationPassed = $true
    $cosignPassed      = $true

    # ── 1. GitHub Attestation ─────────────────────────────────────────────────
    Write-Header "1/2  GitHub Attestation  (gh attestation verify)"
    Write-Info "Subject: $binaryPath"
    $out = & gh attestation verify $binaryPath --repo $Repo 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Pass "Build provenance verified"
    } else {
        Write-Fail "Attestation verification failed"
        $out | ForEach-Object { Write-Host "         $_" -ForegroundColor DarkRed }
        $attestationPassed = $false
    }

    # ── 2. Cosign keyless signature ───────────────────────────────────────────
    Write-Header "2/2  Cosign Keyless Signature  ($cosignCmd verify-blob)"

    $workflowRef = "https://github.com/$Repo/.github/workflows/release.yml@refs/tags/$Tag"
    $oidcIssuer  = "https://token.actions.githubusercontent.com"
    Write-Info "Certificate identity: $workflowRef"
    Write-Info "OIDC issuer:          $oidcIssuer"

    # The .bundle is not installed by winget; fetch it to a temp dir and remove it.
    $tmpDir = Join-Path ([System.IO.Path]::GetTempPath()) ("wtop-verify-" + [System.Guid]::NewGuid().ToString("N"))
    try {
        New-Item -ItemType Directory -Force -Path $tmpDir | Out-Null
        Write-Info "Downloading bundle: $bundleName"
        & gh release download $Tag --repo $Repo --pattern $bundleName --dir $tmpDir --clobber 2>&1 | Out-Null
        $bundlePath = Join-Path $tmpDir $bundleName

        if ($LASTEXITCODE -ne 0 -or -not (Test-Path -LiteralPath $bundlePath)) {
            Write-Fail "Could not download $bundleName from release $Tag"
            $cosignPassed = $false
        } else {
            $out = & $cosignCmd verify-blob `
                --bundle        $bundlePath `
                --certificate-identity $workflowRef `
                --certificate-oidc-issuer $oidcIssuer `
                $binaryPath 2>&1
            if ($LASTEXITCODE -eq 0) {
                Write-Pass "Cosign signature verified"
            } else {
                Write-Fail "Cosign verification failed"
                $out | ForEach-Object { Write-Host "         $_" -ForegroundColor DarkRed }
                $cosignPassed = $false
            }
        }
    } finally {
        if (Test-Path -LiteralPath $tmpDir) {
            Remove-Item -Recurse -Force -LiteralPath $tmpDir -ErrorAction SilentlyContinue
        }
    }

    # ── Summary ───────────────────────────────────────────────────────────────
    Write-Header "Summary  —  $Tag  (winget install)"

    $overallPass = $true
    if ($attestationPassed) { Write-Pass "GitHub Attestation (gh attestation verify)" } else { Write-Fail "GitHub Attestation (gh attestation verify)"; $overallPass = $false }
    if ($cosignPassed)      { Write-Pass "Cosign Signature   (cosign verify-blob)"    } else { Write-Fail "Cosign Signature   (cosign verify-blob)";    $overallPass = $false }
    Write-Info "SBOM Integrity     (skipped — not installed by winget)"

    Write-Host ""
    if ($overallPass) {
        Write-Host "  winget-installed wtop $Tag passed all verification checks." -ForegroundColor Green
        exit 0
    } else {
        Write-Host "  One or more checks failed. See details above." -ForegroundColor Red
        exit 1
    }
}

# ── Resolve release ───────────────────────────────────────────────────────────
Write-Header "Fetching release metadata"

$ghViewArgs = @("release", "view", "--repo", $Repo, "--json", "tagName,assets")
if ($Tag) { $ghViewArgs = @("release", "view", $Tag, "--repo", $Repo, "--json", "tagName,assets") }

$releaseJson = & gh @ghViewArgs 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Fail "Could not fetch release$(if ($Tag) { " $Tag" }): $releaseJson"
    exit 1
}
$release = $releaseJson | ConvertFrom-Json
$Tag     = $release.tagName

Write-Pass "Release: $Tag  ($($release.assets.Count) assets)"

# ── Download assets ───────────────────────────────────────────────────────────
Write-Header "Downloading assets to downloads/"

$downloadDir = Join-Path $PSScriptRoot "downloads"
New-Item -ItemType Directory -Force -Path $downloadDir | Out-Null

foreach ($asset in $release.assets) {
    $dest = Join-Path $downloadDir $asset.name
    if ((Test-Path $dest) -and -not $Force) {
        Write-Info "Cached:     $($asset.name)"
        continue
    }
    Write-Info "Downloading: $($asset.name)"
    & gh release download $Tag --repo $Repo --pattern $asset.name --dir $downloadDir --clobber 2>&1 | Out-Null
    if ($LASTEXITCODE -ne 0) {
        Write-Fail "Download failed: $($asset.name)"
        exit 1
    }
}
Write-Pass "All assets ready"

# ── Identify asset groups ─────────────────────────────────────────────────────
$binaries = Get-ChildItem $downloadDir -Filter "*.exe" | Sort-Object Name
$sbomFile = Get-ChildItem $downloadDir -Filter "*-sbom.cdx.json" | Select-Object -First 1

if ($binaries.Count -eq 0) {
    Write-Fail "No .exe binaries found in downloads/"
    exit 1
}

# ── Result tracking ───────────────────────────────────────────────────────────
$attestationPassed = $true
$cosignPassed      = $true
$sbomPassed        = $true

# ─────────────────────────────────────────────────────────────────────────────
# 1. GitHub Attestation Verify
# ─────────────────────────────────────────────────────────────────────────────
Write-Header "1/3  GitHub Attestation  (gh attestation verify)"

foreach ($bin in $binaries) {
    Write-Info "Subject: $($bin.Name)"
    $out = & gh attestation verify $bin.FullName --repo $Repo 2>&1
    if ($LASTEXITCODE -eq 0) {
        Write-Pass $bin.Name
    } else {
        Write-Fail $bin.Name
        $out | ForEach-Object { Write-Host "         $_" -ForegroundColor DarkRed }
        $attestationPassed = $false
    }
}

# ─────────────────────────────────────────────────────────────────────────────
# 2. Cosign keyless verify-blob
# ─────────────────────────────────────────────────────────────────────────────
Write-Header "2/3  Cosign Keyless Signature  ($cosignCmd verify-blob)"

# The certificate identity is the exact workflow ref that signed the binary.
$workflowRef = "https://github.com/$Repo/.github/workflows/release.yml@refs/tags/$Tag"
$oidcIssuer  = "https://token.actions.githubusercontent.com"

Write-Info "Certificate identity: $workflowRef"
Write-Info "OIDC issuer:          $oidcIssuer"

foreach ($bin in $binaries) {
    $bundlePath = Join-Path $downloadDir "$($bin.Name).bundle"

    if (-not (Test-Path $bundlePath)) {
        Write-Fail "$($bin.Name) — bundle file not found: $($bin.Name).bundle"
        $cosignPassed = $false
        continue
    }

    Write-Info "Subject: $($bin.Name)"
    $out = & $cosignCmd verify-blob `
        --bundle        $bundlePath `
        --certificate-identity $workflowRef `
        --certificate-oidc-issuer $oidcIssuer `
        $bin.FullName 2>&1

    if ($LASTEXITCODE -eq 0) {
        Write-Pass $bin.Name
    } else {
        Write-Fail $bin.Name
        $out | ForEach-Object { Write-Host "         $_" -ForegroundColor DarkRed }
        $cosignPassed = $false
    }
}

# ─────────────────────────────────────────────────────────────────────────────
# 3. SBOM Validation (CycloneDX)
# ─────────────────────────────────────────────────────────────────────────────
Write-Header "3/3  SBOM Integrity  (CycloneDX JSON)"

# NOTE: The release workflow does not cosign-sign the SBOM, so we cannot do
# cosign verify-blob on it. Trust is implicit (HTTPS download from GitHub
# Releases). Adding a .bundle for the SBOM in the release workflow would close
# that gap.

if (-not $sbomFile) {
    Write-Fail "No *-sbom.cdx.json found in downloads/"
    $sbomPassed = $false
} else {
    Write-Info "File: $($sbomFile.Name)"

    # a) Valid JSON
    $sbom = $null
    try {
        $sbom = Get-Content $sbomFile.FullName -Raw | ConvertFrom-Json -ErrorAction Stop
        Write-Pass "Valid JSON"
    } catch {
        Write-Fail "Not valid JSON: $_"
        $sbomPassed = $false
    }

    if ($null -ne $sbom) {
        # b) CycloneDX format marker
        if ($sbom.bomFormat -eq "CycloneDX") {
            Write-Pass "bomFormat = CycloneDX"
        } else {
            Write-Fail "Unexpected bomFormat: '$($sbom.bomFormat)'"
            $sbomPassed = $false
        }

        # c) specVersion present
        if ($sbom.specVersion) {
            Write-Pass "specVersion = $($sbom.specVersion)"
        } else {
            Write-Fail "Missing specVersion"
            $sbomPassed = $false
        }

        # d) metadata.component.version matches the release tag
        $sbomVersion = $sbom.metadata?.component?.version
        if ($sbomVersion -eq $Tag) {
            Write-Pass "metadata.component.version = $Tag"
        } elseif ($sbomVersion) {
            Write-Fail "Version mismatch: SBOM has '$sbomVersion', expected '$Tag'"
            $sbomPassed = $false
        } else {
            Write-Fail "metadata.component.version is absent"
            $sbomPassed = $false
        }

        # e) Non-empty component list
        $compCount = if ($sbom.components) { @($sbom.components).Count } else { 0 }
        if ($compCount -gt 0) {
            Write-Pass "$compCount dependency components listed"
        } else {
            Write-Fail "No components found — SBOM may be empty or malformed"
            $sbomPassed = $false
        }

        # f) License coverage (warn only — not all upstreams publish SPDX IDs)
        if ($compCount -gt 0) {
            $unlicensed = @($sbom.components | Where-Object {
                $lic = $_.PSObject.Properties['licenses']
                -not $lic -or @($lic.Value).Count -eq 0
            }).Count
            if ($unlicensed -eq 0) {
                Write-Pass "All $compCount components have license data"
            } else {
                Write-Warn "$unlicensed of $compCount components lack license data"
            }
        }

        # g) cyclonedx-cli schema validation (when installed)
        if ($hasCycloneDX) {
            Write-Info "Running: cyclonedx validate"
            $out = & cyclonedx validate --input-file $sbomFile.FullName --input-format json 2>&1
            if ($LASTEXITCODE -eq 0) {
                Write-Pass "cyclonedx-cli schema validation"
            } else {
                Write-Fail "cyclonedx-cli schema validation failed"
                $out | ForEach-Object { Write-Host "         $_" -ForegroundColor DarkRed }
                $sbomPassed = $false
            }
        }
    }
}

# ── Summary ───────────────────────────────────────────────────────────────────
Write-Header "Summary  —  $Tag"

$overallPass = $true
if ($attestationPassed) { Write-Pass "GitHub Attestation (gh attestation verify)" } else { Write-Fail "GitHub Attestation (gh attestation verify)"; $overallPass = $false }
if ($cosignPassed)      { Write-Pass "Cosign Signature   (cosign verify-blob)"    } else { Write-Fail "Cosign Signature   (cosign verify-blob)";    $overallPass = $false }
if ($sbomPassed)        { Write-Pass "SBOM Integrity     (CycloneDX)"             } else { Write-Fail "SBOM Integrity     (CycloneDX)";             $overallPass = $false }

Write-Host ""
if ($overallPass) {
    Write-Host "  Release $Tag passed all verification checks." -ForegroundColor Green
    exit 0
} else {
    Write-Host "  One or more checks failed. See details above." -ForegroundColor Red
    exit 1
}
