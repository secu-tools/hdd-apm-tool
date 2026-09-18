# Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
# SPDX-License-Identifier: MIT
# HDD APM Tool build script for Windows PowerShell.
# See "Building" in README.md for usage and flags.
#
# Filename convention: hdd-apm-tool_<VERSION>-<OS>-<ARCH>[.exe]
param(
    [switch]$windows,
    [switch]$linux,
    [switch]$darwin,
    [switch]$amd64,
    [switch]$arm64,
    [switch]$all,
    [switch]$native,
    [switch]$test,
    [switch]$testall,
    [switch]$integration,
    [switch]$teste2e,
    [switch]$testsmoke,
    [switch]$testscripts,
    [switch]$coverage,
    [switch]$clean,
    [switch]$deb,
    [switch]$rpm,
    # Anything not matched above. build.sh rejects unknown flags; without
    # this PowerShell would silently ignore a typo and run a default build.
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Rest
)

# Show-Usage prints the same flag list as build.sh.
function Show-Usage {
    @"
Usage: build.ps1 [targets] [actions]

Targets:
  -windows -linux -darwin    select platform(s)
  -amd64 -arm64              select architecture(s)
  -all                       every platform and architecture
  -native                    this host's platform and architecture only

Actions:
  -test          unit tests + fuzz seed corpus
  -integration   integration tests (none in this project)
  -teste2e       end-to-end tests (none in this project)
  -testsmoke     smoke tests (none in this project)
  -testscripts   the build scripts, against a copy of the tree
  -testall       every suite above, in order
  -coverage      unit tests with an HTML coverage report
  -clean         remove build artifacts
  -deb -rpm      package linux builds (combine with -linux or -all)
"@ | Write-Host
}

if ($Rest) {
    Write-Host "Unknown argument: $($Rest -join ' ')"
    Write-Host ""
    Show-Usage
    exit 1
}

$Binary = "hdd-apm-tool"

# Outside a git checkout (a source tarball, or the copy the script tests build
# in) git writes to stderr. Errors are tolerated explicitly here so the lookup
# stays harmless if this script ever adopts $ErrorActionPreference = "Stop",
# under which a bare stderr write would end the run before it started.
$Commit = ""
$prevErrorAction = $ErrorActionPreference
$ErrorActionPreference = "Continue"
try { $Commit = (git rev-parse --short HEAD 2>$null) } catch { $Commit = "" }
$ErrorActionPreference = $prevErrorAction
if (-not $Commit) { $Commit = "dev" }

# Read base version from version/version_base.txt
$VersionBaseFile = Join-Path $PSScriptRoot "version\version_base.txt"
if (Test-Path $VersionBaseFile) {
    $Version = (Get-Content $VersionBaseFile -ErrorAction SilentlyContinue | Select-Object -First 1).Trim()
}
if (-not $Version) { $Version = "1.0.0" }
if ($env:VERSION) { $Version = $env:VERSION }

# Auto-increment build number (or use BUILD_NUMBER env var to pin an exact value)
# When $env:BUILD_NUMBER is set, the file is NOT written -- callers manage versioning.
$BuildNumberFile = Join-Path $PSScriptRoot "version\build_number.txt"
$SkipBuildNumberBump = $false
if ($env:BUILD_NUMBER) {
    # Only the digits count, and a value with none is 0, which is what build.sh
    # does with the same input; "007" is 7 on both.
    $rawEnv = $env:BUILD_NUMBER -replace '[^0-9]', ''
    $BuildNumber = if ($rawEnv) { [int]$rawEnv } else { 0 }
    $SkipBuildNumberBump = $true
} else {
    $BuildNumber = 0
    if (Test-Path $BuildNumberFile) {
        $raw = (Get-Content $BuildNumberFile -Raw -ErrorAction SilentlyContinue).Trim() -replace '[^0-9]', ''
        if ($raw) { $BuildNumber = [int]$raw }
    }
}
# Take-BuildNumber is called only once a build is actually going to happen.
# This build takes the number the file holds and leaves the next one behind:
# the convention build.sh, the release workflow and the sibling projects
# share. Taking the number the file was bumped TO instead would stamp this
# build one ahead of the release built from the same starting file.
#
# It is NOT called for -test, -clean and the other actions, because a run that
# produces no binary must not consume a version.
#
# A named system Mutex keeps concurrent PowerShell builds from taking the same
# number or corrupting the file.
function Take-BuildNumber {
    if ($SkipBuildNumberBump) { return }
    $mtx = [System.Threading.Mutex]::new($false, "Global\HddApmToolBuildNumber")
    try {
        $null = $mtx.WaitOne()
        # Re-read under the lock to handle the TOCTOU window.
        $lockedRaw = (Get-Content $BuildNumberFile -Raw -ErrorAction SilentlyContinue).Trim() -replace '[^0-9]', ''
        $lockedNum = if ($lockedRaw) { [int]$lockedRaw } else { 0 }
        $script:BuildNumber = $lockedNum
        Set-Content $BuildNumberFile ($lockedNum + 1)
    } finally {
        $mtx.ReleaseMutex()
        $mtx.Dispose()
    }
    $script:FullVersion = "$Version.$script:BuildNumber"
    $script:LDFlags = "-X ${Module}.version=$Version -X ${Module}.commit=$Commit -X ${Module}.buildNumber=$script:BuildNumber"
}

$FullVersion = "$Version.$BuildNumber"
$Module      = "github.com/secu-tools/hdd-apm-tool/internal/app"
$LDFlags     = "-X ${Module}.version=$Version -X ${Module}.commit=$Commit -X ${Module}.buildNumber=$BuildNumber"
$BuildDir    = "build"

Write-Host "HDD APM Tool Build Script" -ForegroundColor Cyan
Write-Host "========================"
Write-Host "Version: $FullVersion"
Write-Host "Commit:  $Commit"
Write-Host ""

# -- Detect toolchain ------------------------------------------------

# Find-Nfpm returns the nfpm executable: the one on PATH, or the one go install
# leaves in GOBIN (GOPATH\bin by default), which is not always on PATH.
function Find-Nfpm {
    $cmd = Get-Command nfpm -ErrorAction SilentlyContinue
    if ($cmd) { return $cmd.Source }
    $gobin = (go env GOBIN)
    if (-not $gobin) { $gobin = Join-Path (go env GOPATH) "bin" }
    foreach ($name in "nfpm.exe", "nfpm") {
        $cand = Join-Path $gobin $name
        if (Test-Path $cand) { return $cand }
    }
    return $null
}

# nfpm (needed for -deb / -rpm packaging)
$NfpmAvailable = $false
$NfpmPath = Find-Nfpm
if ($NfpmPath) {
    $NfpmAvailable = $true
    Write-Host "nfpm:    found ($NfpmPath)" -ForegroundColor Green
} elseif ($deb.IsPresent -or $rpm.IsPresent) {
    Write-Host "nfpm:    not found -- auto-installing..." -ForegroundColor Yellow
    # The install's own output is kept, so a failure says why.
    go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest
    if ($LASTEXITCODE -ne 0) {
        Write-Host "nfpm:    auto-install failed" -ForegroundColor Red
        Write-Host "         Install manually: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest" -ForegroundColor Yellow
        exit 1
    }
    $NfpmPath = Find-Nfpm
    if (-not $NfpmPath) {
        Write-Host "nfpm:    installed, but not found in GOBIN or on PATH" -ForegroundColor Red
        exit 1
    }
    $NfpmAvailable = $true
    Write-Host "nfpm:    installed ($NfpmPath)" -ForegroundColor Green
}

Write-Host ""

# Handle clean / test / coverage first
if ($clean) {
    Write-Host "Cleaning..." -ForegroundColor Yellow
    Remove-Item $BuildDir -Recurse -ErrorAction SilentlyContinue
    Remove-Item coverage.out -ErrorAction SilentlyContinue
    Remove-Item coverage.html -ErrorAction SilentlyContinue
    Write-Host "Clean complete."
    exit 0
}

# No-Suite: this project has no such suite. The flag is accepted so the same
# commands work across every project; it reports and succeeds.
function No-Suite($name) {
    Write-Host "No $name tests in this project."
    exit 0
}

if ($integration) { No-Suite "integration" }
if ($teste2e)     { No-Suite "end-to-end" }
if ($testsmoke)   { No-Suite "smoke" }

# Invoke-Suite <label> <scriptblock> -- run one suite, ending the script if it
# fails.
function Invoke-Suite($label, [scriptblock]$body) {
    Write-Host ""
    Write-Host "$label..." -ForegroundColor Cyan
    & $body
    if ($LASTEXITCODE -ne 0) {
        Write-Host "$label failed" -ForegroundColor Red
        exit 1
    }
}

if ($test) {
    Write-Host "Running unit tests + fuzz seed corpus..." -ForegroundColor Cyan
    Invoke-Suite "[1/2] Unit tests" { go test -buildvcs=false -count=1 ./... }
    Invoke-Suite "[2/2] Fuzz seed corpus" { go test -buildvcs=false -count=1 -run '^Fuzz' ./internal/... }
    Write-Host ""
    Write-Host "All tests passed." -ForegroundColor Green
    exit 0
}

if ($testscripts) {
    Write-Host "Running build script tests..." -ForegroundColor Cyan
    go test -tags scripts -count=1 -timeout 900s ./tests/scripts/
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Build script tests failed" -ForegroundColor Red
        exit 1
    }
    Write-Host "Build script tests passed." -ForegroundColor Green
    exit 0
}

if ($testall) {
    Write-Host "Running full suite: unit -> fuzz -> build scripts..." -ForegroundColor Cyan
    Invoke-Suite "[1/3] Unit tests" { go test -buildvcs=false -count=1 ./... }
    Invoke-Suite "[2/3] Fuzz seed corpus" { go test -buildvcs=false -count=1 -run '^Fuzz' ./internal/... }
    Invoke-Suite "[3/3] Build script tests" { go test -tags scripts -count=1 -timeout 900s ./tests/scripts/ }
    Write-Host ""
    Write-Host "Full test suite passed." -ForegroundColor Green
    exit 0
}

if ($coverage) {
    Write-Host "Running tests with coverage ..." -ForegroundColor Cyan
    go test -buildvcs=false ./... -coverprofile=coverage.out -count=1
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Tests failed!" -ForegroundColor Red
        exit 1
    }
    go tool cover -html=coverage.out -o coverage.html
    Write-Host "Coverage report: coverage.html" -ForegroundColor Green
    exit 0
}

# Wipe build directory
if (Test-Path $BuildDir) {
    Remove-Item $BuildDir -Recurse -Force
}

# Determine platforms to build
$osExplicit   = $windows.IsPresent -or $linux.IsPresent -or $darwin.IsPresent
$archExplicit = $amd64.IsPresent   -or $arm64.IsPresent

if ($native) {
    # -native: this host only, whatever it is.
    $hostOS = (go env GOOS)
    $hostArch = (go env GOARCH)
    if ($hostOS -notin @("windows", "linux", "darwin")) {
        Write-Host "Unsupported host platform: $hostOS" -ForegroundColor Red
        exit 1
    }
    if ($hostArch -notin @("amd64", "arm64")) {
        Write-Host "Unsupported host architecture: $hostArch" -ForegroundColor Red
        exit 1
    }
    $selectedOS   = @($hostOS)
    $selectedArch = @($hostArch)
} elseif ($all) {
    $selectedOS   = @("windows", "linux", "darwin")
    $selectedArch = @("amd64", "arm64")
} elseif ($osExplicit -and $archExplicit) {
    $selectedOS = @()
    if ($windows.IsPresent) { $selectedOS += "windows" }
    if ($linux.IsPresent)   { $selectedOS += "linux" }
    if ($darwin.IsPresent)  { $selectedOS += "darwin" }
    $selectedArch = @()
    if ($amd64.IsPresent) { $selectedArch += "amd64" }
    if ($arm64.IsPresent) { $selectedArch += "arm64" }
} elseif ($osExplicit) {
    $selectedOS = @()
    if ($windows.IsPresent) { $selectedOS += "windows" }
    if ($linux.IsPresent)   { $selectedOS += "linux" }
    if ($darwin.IsPresent)  { $selectedOS += "darwin" }
    $selectedArch = @("amd64", "arm64")
} elseif ($amd64.IsPresent -and -not $arm64.IsPresent) {
    $selectedOS   = @("windows", "linux")
    $selectedArch = @("amd64")
} elseif ($arm64.IsPresent -and -not $amd64.IsPresent) {
    $selectedOS   = @("windows", "linux")
    $selectedArch = @("arm64")
} elseif ($amd64.IsPresent -and $arm64.IsPresent) {
    $selectedOS   = @("windows", "linux")
    $selectedArch = @("amd64", "arm64")
} else {
    # Default: windows+linux, amd64 only (darwin is opt-in)
    $selectedOS   = @("windows", "linux")
    $selectedArch = @("amd64")
}

$extMap = @{ "windows" = ".exe"; "linux" = ""; "darwin" = "" }

$platforms = @()
foreach ($os in $selectedOS) {
    foreach ($arch in $selectedArch) {
        $platforms += @{ GOOS = $os; GOARCH = $arch; Ext = $extMap[$os] }
    }
}

# A build is definitely happening now, so take the build number and leave the
# next one in the file.
Take-BuildNumber

Write-Host "Building $($platforms.Count) target(s)..." -ForegroundColor Green

# Clear-GoEnv drops the per-target variables so they do not outlive the script
# in the calling session, whichever way the script ends.
function Clear-GoEnv {
    Remove-Item Env:\GOOS, Env:\GOARCH, Env:\CGO_ENABLED -ErrorAction SilentlyContinue
}

# -- Build function ---------------------------------------------------
function Build-Target($p) {
    $outDir = "$BuildDir/$($p.GOOS)"
    if (-not (Test-Path $outDir)) {
        New-Item -ItemType Directory -Path $outDir -Force | Out-Null
    }
    $output = "$outDir/${Binary}_${FullVersion}-$($p.GOOS)-$($p.GOARCH)$($p.Ext)"
    $env:GOOS        = $p.GOOS
    $env:GOARCH      = $p.GOARCH
    $env:CGO_ENABLED = "0"

    Write-Host "  Building $output..."
    $buildArgs = @("build", "-buildvcs=false", "-trimpath", "-ldflags", "$LDFlags -s -w", "-o", $output, "./")
    & go @buildArgs
    if ($LASTEXITCODE -ne 0) {
        Write-Host "    FAILED: $output" -ForegroundColor Red
        Remove-Item $output -ErrorAction SilentlyContinue
        Clear-GoEnv
        # A compile error ends the run, as it does in build.sh. A partial build
        # that went on to report "Build complete" would be shipped as if whole.
        exit 1
    }
    $size = (Get-Item $output).Length
    Write-Host "    -> $([math]::Round($size/1KB, 1)) KB" -ForegroundColor White
}

# -- nfpm packaging function ------------------------------------------
function Package-Nfpm([string]$binaryPath, [string]$goarch, [string]$format) {
    $archMap = @{
        "amd64" = if ($format -eq "deb") { "amd64" } else { "x86_64" }
        "arm64" = if ($format -eq "deb") { "arm64" } else { "aarch64" }
    }
    $pkgArch = $archMap[$goarch]
    if (-not $pkgArch) { $pkgArch = $goarch }

    $outDir  = Split-Path $binaryPath
    $binName = (Get-Item $binaryPath).Name
    $pkgFile = "$outDir/${binName}.${format}"

    $nfpmYaml = @"
name: hdd-apm-tool
arch: $pkgArch
version: $FullVersion
maintainer: Jack L. (Cpt-JackL) <https://jack-l.com>
description: HDD APM Tool - ATA Advanced Power Management configuration tool for spinning HDDs
homepage: https://github.com/secu-tools/hdd-apm-tool
license: MIT
contents:
  - src: $($binaryPath.Replace('\', '/'))
    dst: /usr/bin/hdd-apm-tool
    file_info:
      mode: 0755
"@

    $tmpYaml = Join-Path $env:TEMP "nfpm_$(Get-Random).yaml"
    Set-Content -Path $tmpYaml -Value $nfpmYaml -Encoding UTF8

    Write-Host "  Packaging $pkgFile..." -ForegroundColor Magenta
    & $NfpmPath pkg --config $tmpYaml --packager $format --target $pkgFile
    $exitCode = $LASTEXITCODE
    Remove-Item $tmpYaml -ErrorAction SilentlyContinue

    if ($exitCode -ne 0) {
        Write-Host "    FAILED: $pkgFile" -ForegroundColor Red
        Clear-GoEnv
        exit 1
    }
    $size = (Get-Item $pkgFile).Length
    Write-Host "    -> $([math]::Round($size/1KB, 1)) KB" -ForegroundColor Magenta
}

# -- Main build loop --------------------------------------------------
foreach ($p in $platforms) {
    Build-Target $p
}

# -- Package Linux binaries with nfpm if -deb or -rpm requested -------
if ($NfpmAvailable -and ($deb.IsPresent -or $rpm.IsPresent)) {
    Write-Host ""
    Write-Host "Packaging Linux binaries..." -ForegroundColor Magenta

    $linuxBinaries = Get-ChildItem "$BuildDir/linux" -File -ErrorAction SilentlyContinue |
        Where-Object { $_.Name -match '^hdd-apm-tool_.*-linux-(amd64|arm64)$' }

    foreach ($bin in $linuxBinaries) {
        if ($bin.Name -match '-linux-(amd64|arm64)$') {
            $arch = $Matches[1]
            if ($deb.IsPresent) { Package-Nfpm $bin.FullName $arch "deb" }
            if ($rpm.IsPresent) { Package-Nfpm $bin.FullName $arch "rpm" }
        }
    }
}

# Reset environment
Clear-GoEnv

Write-Host ""
Write-Host "Build complete. Output in $BuildDir/" -ForegroundColor Green
Get-ChildItem $BuildDir -Recurse -File | ForEach-Object {
    Write-Host "  $($_.FullName.Replace((Get-Location).Path + '\', ''))"
}
