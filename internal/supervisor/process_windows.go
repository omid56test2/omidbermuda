//go:build windows

package supervisor

import (
	"os"
	"os/exec"
)

// Windows does not support Unix process groups/Setpgid. The Xray process is
// started as a normal child process and receives an interrupt on graceful stop.
func configureProcess(cmd *exec.Cmd) error { return nil }

func terminateProcess(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Signal(os.Interrupt)
}
