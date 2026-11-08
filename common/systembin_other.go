//go:build !windows

package common

func SystemBinary(name string) string { return name }

func PowerShellPath() string { return "powershell" }

func SchtasksPath() string { return "schtasks" }

func TarPath() string { return "tar" }
