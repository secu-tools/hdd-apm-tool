// Copyright (c) 2026 Jack L. (Cpt-JackL) (https://jack-l.com)
// SPDX-License-Identifier: MIT

//go:build windows

package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"

	"github.com/secu-tools/hdd-apm-tool/internal/logging"
)

// maybeRunAsWindowsService checks whether the process was started by the Windows
// Service Control Manager. If so, it runs the tool inside the SCM service loop.
// Returns true when running as a service (the caller should return immediately).
func maybeRunAsWindowsService(svcName, logDir string, apmLevel uint8, dryRun bool) bool {
	isService, err := svc.IsWindowsService()
	if err != nil || !isService {
		return false
	}
	if err := svc.Run(svcName, &winSvcHandler{
		svcName:  svcName,
		logDir:   logDir,
		apmLevel: apmLevel,
		dryRun:   dryRun,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR | Windows service %q exited: %v\n", svcName, err)
	}
	return true
}

// winSvcHandler implements the windows/svc.Handler interface so that the binary
// can run as a first-class Windows Service.
type winSvcHandler struct {
	svcName  string
	logDir   string
	apmLevel uint8
	dryRun   bool
}

// Execute is called by the SCM dispatcher. It reports service state back to the
// SCM and calls runWithContext to do the actual work.
func (h *winSvcHandler) Execute(_ []string, r <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Log to file when --logdir was supplied at install time, otherwise stdout.
	var logr *logging.Logger
	if h.logDir != "" {
		if err := logging.SetLogDir(h.logDir); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR | Failed to set log directory: %v\n", err)
			return false, 1
		}
		var ferr error
		logr, ferr = logging.New("hdd-apm-tool.log", logging.DefaultConfig(), "svc")
		if ferr != nil {
			logr = logging.NewStdoutOnly(logging.DefaultConfig(), "svc")
		}
	} else {
		logr = logging.NewStdoutOnly(logging.DefaultConfig(), "svc")
	}
	defer logr.Close()

	cfg := RunConfig{
		APMLevel: h.apmLevel,
		DryRun:   h.dryRun,
		LogDir:   h.logDir,
		SvcName:  h.svcName,
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		runWithContext(ctx, cfg, logr)
	}()

	status <- svc.Status{
		State:   svc.Running,
		Accepts: svc.AcceptStop | svc.AcceptShutdown,
	}

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				<-done
				return false, 0
			}
		case <-done:
			// Work is complete. Stay in Running state briefly before stopping so
			// that the Windows Services MMC does not show the "started and then
			// stopped" warning when the service is started manually. The dwell
			// has no effect on autostart boots.
			select {
			case <-time.After(2 * time.Second):
			case c := <-r:
				if c.Cmd == svc.Stop || c.Cmd == svc.Shutdown {
					status <- svc.Status{State: svc.StopPending}
				}
			}
			return false, 0
		}
	}
}

// serviceContext returns a context that is cancelled when the user presses
// Ctrl+C in interactive (non-SCM) mode.
func serviceContext() context.Context {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	_ = stop
	return ctx
}

// isElevated reports whether the process is running with administrator privileges.
func isElevated() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)
	member, err := windows.Token(0).IsMember(sid)
	if err != nil {
		return false
	}
	return member
}

// quoteWindowsArg quotes a single command-line argument following the
// CommandLineToArgvW rules so that arguments containing whitespace or double
// quotes survive the round trip through the elevated re-launch.
func quoteWindowsArg(a string) string {
	return syscall.EscapeArg(a)
}

// requestElevation re-launches the current process with administrator privileges
// using the Windows ShellExecute "runas" verb (triggers a UAC elevation dialog).
// If the re-launch succeeds this function calls os.Exit(0) to terminate the
// current non-elevated process. This function never returns normally.
func requestElevation() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR | Cannot determine executable path: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please run as Administrator (right-click -> Run as administrator).\n")
		os.Exit(1)
	}

	// Re-assemble the argument list, quoting arguments as needed.
	var args string
	if len(os.Args) > 1 {
		parts := make([]string, 0, len(os.Args)-1)
		for _, a := range os.Args[1:] {
			parts = append(parts, quoteWindowsArg(a))
		}
		args = strings.Join(parts, " ")
	}

	verbPtr, _ := windows.UTF16PtrFromString("runas")
	filePtr, _ := windows.UTF16PtrFromString(exe)
	var argsPtr *uint16
	if args != "" {
		argsPtr, _ = windows.UTF16PtrFromString(args)
	}

	// ShellExecuteW returns when the new elevated process has been created.
	// NewLazySystemDLL restricts the DLL search to the System32 directory,
	// preventing DLL search-path hijacking from the current directory.
	ret, _, _ := windows.NewLazySystemDLL("shell32.dll").
		NewProc("ShellExecuteW").
		Call(0,
			uintptr(unsafe.Pointer(verbPtr)),
			uintptr(unsafe.Pointer(filePtr)),
			uintptr(unsafe.Pointer(argsPtr)),
			0,
			windows.SW_NORMAL)
	// ShellExecuteW returns an HINSTANCE > 32 on success.
	if ret <= 32 {
		fmt.Fprintf(os.Stderr, "ERROR | UAC elevation failed (ShellExecute returned %d).\n", ret)
		fmt.Fprintf(os.Stderr, "Please run as Administrator (right-click -> Run as administrator).\n")
		os.Exit(1)
	}
	os.Exit(0)
}
