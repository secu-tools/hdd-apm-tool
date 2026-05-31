@echo off
REM Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
REM SPDX-License-Identifier: MIT
REM HDD APM Tool build script for Windows CMD
REM This is a convenience wrapper that launches build.ps1 with PowerShell.
REM All arguments are forwarded.
REM
REM Examples:
REM   build.cmd                    Build windows/amd64 + linux/amd64
REM   build.cmd -all               Build all platform/arch combos (linux+windows+darwin)
REM   build.cmd -windows           Build windows/amd64 + windows/arm64
REM   build.cmd -linux             Build linux/amd64 + linux/arm64
REM   build.cmd -darwin            Build darwin/amd64 + darwin/arm64
REM   build.cmd -test              Run unit tests
REM   build.cmd -coverage          Run tests with coverage report
REM   build.cmd -clean             Clean build artifacts
REM   build.cmd -linux -deb -rpm   Build linux binaries and create .deb/.rpm packages
REM   build.cmd -windows -arm64    Build Windows amd64 + arm64
REM
REM Binaries: build/<OS>/hdd-apm-tool_<VER>-<OS>-<ARCH>[.exe]

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
