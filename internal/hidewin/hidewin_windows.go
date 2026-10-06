//go:build windows

// Package hidewin keeps the children this program starts, with their output
// captured, from opening a console window on Windows.
package hidewin

import (
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// Hide makes cmd start without a console window. Its output still flows
// through whatever Stdout and Stderr hold. Call it before cmd starts.
func Hide(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	cmd.SysProcAttr.HideWindow = true
	return cmd
}
