# Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
# SPDX-License-Identifier: MIT
# HDD APM Tool build script for Windows PowerShell
#
# Usage:
#   .\build.ps1                   # Build windows/amd64 + linux/amd64
#   .\build.ps1 -windows          # Build windows/amd64 + windows/arm64
#   .\build.ps1 -linux            # Build linux/amd64 + linux/arm64
#   .\build.ps1 -darwin           # Build darwin/amd64 + darwin/arm64
#   .\build.ps1 -amd64            # Build all platforms for amd64 only
#   .\build.ps1 -arm64            # Build all platforms for arm64 only
#   .\build.ps1 -linux -amd64     # Build linux/amd64 only
#   .\build.ps1 -linux -arm64     # Build linux/arm64 only
#   .\build.ps1 -windows -amd64   # Build windows/amd64 only
#   .\build.ps1 -windows -arm64   # Build windows/arm64 only
#   .\build.ps1 -darwin -amd64    # Build darwin/amd64 only
#   .\build.ps1 -darwin -arm64    # Build darwin/arm64 only
#   .\build.ps1 -all              # Build all platform/arch combinations (linux+windows+darwin)
#   .\build.ps1 -test             # Run unit tests
#   .\build.ps1 -coverage         # Run tests with coverage
#   .\build.ps1 -clean            # Clean build artifacts
#   .\build.ps1 -linux -deb       # Build linux + create .deb packages
#   .\build.ps1 -linux -rpm       # Build linux + create .rpm packages
#   .\build.ps1 -linux -deb -rpm  # Build linux + both .deb and .rpm
#
# All builds use CGO_ENABLED=0 (pure Go).
#
# Filename convention: hdd-apm-tool_<VERSION>-<OS>-<ARCH>[.exe]
param(
    [switch]$windows,
    [switch]$linux,
    [switch]$darwin,
    [switch]$amd64,
    [switch]$arm64,
    [switch]$all,
    [switch]$test,
    [switch]$coverage,
    [switch]$clean,
    [switch]$deb,
    [switch]$rpm
)

$Binary = "hdd-apm-tool"
$Commit = try { git rev-parse --short HEAD 2>$null } catch { "dev" }
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
    $BuildNumber = [int]$env:BUILD_NUMBER
    $SkipBuildNumberBump = $true
} else {
    $BuildNumber = 0
    if (Test-Path $BuildNumberFile) {
        $raw = (Get-Content $BuildNumberFile -Raw -ErrorAction SilentlyContinue).Trim() -replace '[^0-9]', ''
        if ($raw) { $BuildNumber = [int]$raw }
    }
}
if (-not $SkipBuildNumberBump) {
    # Use a named system Mutex so concurrent PowerShell build processes do not
    # produce duplicate build numbers or corrupt the file.
    $mtx = [System.Threading.Mutex]::new($false, "Global\HddApmToolBuildNumber")
    try {
        $null = $mtx.WaitOne()
        # Re-read under the lock to handle the TOCTOU window.
        $lockedRaw = (Get-Content $BuildNumberFile -Raw -ErrorAction SilentlyContinue).Trim() -replace '[^0-9]', ''
        $lockedNum = if ($lockedRaw) { [int]$lockedRaw } else { 0 }
        $BuildNumber = $lockedNum + 1
        Set-Content $BuildNumberFile $BuildNumber
    } finally {
        $mtx.ReleaseMutex()
        $mtx.Dispose()
    }
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

# nfpm (needed for -deb / -rpm packaging)
$NfpmAvailable = $false
$NfpmPath = Get-Command nfpm -ErrorAction SilentlyContinue
if ($NfpmPath) {
    $NfpmAvailable = $true
    Write-Host "nfpm:    found ($($NfpmPath.Source))" -ForegroundColor Green
} else {
    if ($deb.IsPresent -or $rpm.IsPresent) {
        Write-Host "nfpm:    not found -- auto-installing..." -ForegroundColor Yellow
        & go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest 2>&1 | Out-Null
        $NfpmPath = Get-Command nfpm -ErrorAction SilentlyContinue
        if ($NfpmPath) {
            $NfpmAvailable = $true
            Write-Host "nfpm:    installed ($($NfpmPath.Source))" -ForegroundColor Green
        } else {
            Write-Host "nfpm:    auto-install failed" -ForegroundColor Red
            Write-Host "         Install manually: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@latest" -ForegroundColor Yellow
            exit 1
        }
    }
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

if ($test) {
    Write-Host "Running tests ..." -ForegroundColor Cyan
    go test -buildvcs=false -count=1 ./...
    if ($LASTEXITCODE -ne 0) {
        Write-Host "Tests failed!" -ForegroundColor Red
        exit 1
    }
    Write-Host "Tests passed." -ForegroundColor Green
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

if ($all) {
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

Write-Host "Building $($platforms.Count) target(s)..." -ForegroundColor Green

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
        return $false
    }
    $size = (Get-Item $output).Length
    Write-Host "    -> $([math]::Round($size/1KB, 1)) KB" -ForegroundColor White
    return $true
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
    & nfpm pkg --config $tmpYaml --packager $format --target $pkgFile
    $exitCode = $LASTEXITCODE
    Remove-Item $tmpYaml -ErrorAction SilentlyContinue

    if ($exitCode -ne 0) {
        Write-Host "    FAILED: $pkgFile" -ForegroundColor Red
        return
    }
    $size = (Get-Item $pkgFile).Length
    Write-Host "    -> $([math]::Round($size/1KB, 1)) KB" -ForegroundColor Magenta
}

# -- Main build loop --------------------------------------------------
$buildSuccess = 0
$buildFailed  = 0
foreach ($p in $platforms) {
    if (Build-Target $p) { $buildSuccess++ } else { $buildFailed++ }
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
Remove-Item Env:\GOOS        -ErrorAction SilentlyContinue
Remove-Item Env:\GOARCH      -ErrorAction SilentlyContinue
Remove-Item Env:\CGO_ENABLED -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "Build complete: $buildSuccess succeeded, $buildFailed failed." -ForegroundColor $(if ($buildFailed -eq 0) { "Green" } else { "Red" })
if ($buildFailed -gt 0) { exit 1 }
Write-Host ""
Write-Host "Output in $BuildDir/" -ForegroundColor Green
Get-ChildItem $BuildDir -Recurse -File | ForEach-Object {
    Write-Host "  $($_.FullName.Replace((Get-Location).Path + '\', ''))"
}
