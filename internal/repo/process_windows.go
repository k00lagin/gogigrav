package repo

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func hideConsole(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }

func executable(tool string) string {
	if tool != "gh" {
		return tool
	}
	if path, err := exec.LookPath(tool); err == nil {
		return path
	}
	for _, base := range []string{filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"), os.Getenv("ProgramFiles")} {
		path := filepath.Join(base, "GitHub CLI", "gh.exe")
		if filepath.IsAbs(path) {
			if _, err := os.Stat(path); err == nil {
				return path
			}
		}
	}
	return tool
}
