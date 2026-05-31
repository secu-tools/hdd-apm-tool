// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build !windows

package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
)

// maybeRunAsWindowsService is a no-op on non-Windows platforms.
func maybeRunAsWindowsService(_, _ string, _ uint8, _ bool) bool {
	return false
}

// serviceContext returns a context that is cancelled on SIGTERM or SIGINT.
func serviceContext() context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, os.Interrupt)
	_ = stop
	return ctx
}

// isElevated reports whether the current process has root privileges.
func isElevated() bool {
	return os.Geteuid() == 0
}

// requestElevation prints a message asking the user to re-run with sudo, then
// exits. On Linux/macOS there is no automatic re-launch equivalent to UAC.
// This function never returns normally.
func requestElevation() {
	fmt.Fprintf(os.Stderr, "ERROR | This tool requires root privileges.\n")
	fmt.Fprintf(os.Stderr, "Please run with sudo: sudo %s\n",
		strings.Join(os.Args, " "))
	os.Exit(1)
}
