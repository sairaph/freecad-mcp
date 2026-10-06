//go:build !windows

// Package hidewin keeps the children this program starts, with their output
// captured, from opening a console window on Windows.
package hidewin

import "os/exec"

// Hide does nothing off Windows.
func Hide(cmd *exec.Cmd) *exec.Cmd { return cmd }
