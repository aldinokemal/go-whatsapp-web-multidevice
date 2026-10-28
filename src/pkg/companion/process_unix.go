//go:build unix

package companion

import (
	"os/exec"
	"syscall"
)

// Terminal Ctrl-C belongs to the controller. Adapter shutdown uses private EOF
// after the runner has arranged phone cleanup, preserving server locks meanwhile.
func isolateProcess(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true} }
