@echo off
REM Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
REM SPDX-License-Identifier: MIT
REM HDD APM Tool build script for Windows CMD.
REM Forwards all arguments to build.ps1 - see "Building" in README.md for usage.

powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0build.ps1" %*
