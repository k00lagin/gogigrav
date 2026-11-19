//go:build !windows

package repo

import "os/exec"

func hideConsole(cmd *exec.Cmd)     {}
func executable(tool string) string { return tool }
