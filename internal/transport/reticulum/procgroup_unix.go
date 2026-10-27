//go:build unix

package reticulum

import (
	"os/exec"
	"syscall"
)

// ownGroup puts the sidecar in a new process group whose id is its pid.
func ownGroup(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }

// killGroup kills the sidecar and anything it spawned.
func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill() // in case the group kill raced the setpgid
	return err
}
