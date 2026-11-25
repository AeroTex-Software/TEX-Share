//go:build !windows

package app

import (
	"os"
	"os/exec"
	"syscall"
	"time"
)

// setProcAttrs starts child processes in their own process group so signals can
// reach helper processes spawned by clamd or cloudflared.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateProcess asks the whole process group to stop, escalating to SIGKILL
// when the process ignores SIGTERM.
func terminateProcess(p *os.Process) {
	if p == nil {
		return
	}
	pid := p.Pid
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	go func() {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			if err := syscall.Kill(pid, 0); err != nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_ = p.Kill()
	}()
}
