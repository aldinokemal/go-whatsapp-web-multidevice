//go:build !unix

package companion

import "os/exec"

func isolateProcess(*exec.Cmd) {}
