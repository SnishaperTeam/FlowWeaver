//go:build windows

package common

import (
	"os"
	"path/filepath"
	"strings"
)

func SystemBinary(name string) string {
	sysroot := strings.TrimSpace(os.Getenv("SystemRoot"))
	if sysroot == "" {
		sysroot = `C:\Windows`
	}
	candidate := filepath.Join(sysroot, "System32", name)
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return name
}

func PowerShellPath() string { return SystemBinary(`WindowsPowerShell\v1.0\powershell.exe`) }

func SchtasksPath() string { return SystemBinary("schtasks.exe") }

func TarPath() string { return SystemBinary("tar.exe") }
