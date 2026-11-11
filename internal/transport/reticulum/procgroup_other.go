//go:build !unix

package reticulum

import "os/exec"

// ownGroup is a no-op where process groups are not available; WaitDelay
// still bounds how long a lingering grandchild can hold the pipes.
func ownGroup(*exec.Cmd) {}

func killGroup(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
