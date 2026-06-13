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

.EXAMPLE
    .\verify-release.ps1
    .\verify-release.ps1 -Tag v0.5.0
#>
param(
    [string]$Repo  = "michaelsanford/wtop",
    [string]$Tag   = "",
    [switch]$Force
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

# Optional: cyclonedx-cli enables formal JSON schema validation of the SBOM.
$hasCycloneDX = [bool](Get-Command "cyclonedx" -ErrorAction SilentlyContinue)
if ($hasCycloneDX) {
    Write-Pass "cyclonedx-cli (schema validation enabled)"
} else {
    Write-Warn "cyclonedx-cli not found — skipping formal schema validation"
    Write-Info "Install: winget install CycloneDX.CLI"
}

if ($prereqsFailed) {
    Write-Host ""
    Write-Host "  Install missing required tools and re-run." -ForegroundColor Red
    exit 1
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
