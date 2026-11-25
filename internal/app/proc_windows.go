//go:build windows

package app

import (
	"os"
	"os/exec"
	"syscall"
)

// setProcAttrs creates the child in its own process group so console control
// events targeted at TEX Share do not reach helper processes unexpectedly.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// terminateProcess forcibly ends the child process on Windows.
func terminateProcess(p *os.Process) {
	if p == nil {
		return
	}
	_ = p.Kill()
}
