package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/SnishaperTeam/snishaper-core/proxy"
)

// PortOccupant 描述占用指定端口的进程信息。
type PortOccupant struct {
	Port int    `json:"port"`
	PID  int    `json:"pid"`
	Name string `json:"name"`
}

// GetPortOccupant 返回占用指定端口的进程信息；端口未被占用时返回 nil。
// 端口落在 Windows 排除端口范围（无进程占用但无法 bind）时返回 PID 为 -1 的系统保留标记。
func (a *App) GetPortOccupant(port int) *PortOccupant {
	pid, err := proxy.FindProcessByPort(port)
	if err != nil || pid <= 0 {
		if proxy.IsPortInExcludedRange(port) {
			return &PortOccupant{Port: port, PID: -1, Name: "Windows 系统保留端口 (Excluded Port Range)"}
		}
		return nil
	}
	name, _ := proxy.GetProcessNameByPID(pid)
	return &PortOccupant{Port: port, PID: pid, Name: name}
}

func ownExeName() string {
	execPath, err := os.Executable()
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(execPath), ".exe")
}

// KillPortOccupant 强制结束占用指定端口的进程（含子进程树）。port 用于安装前的
// 复核：调用方拿到 occupant 到真正点击结束之间，进程可能已退出且 PID 被系统
// 复用——不复核就会以管理员权限误杀无关进程。
func (a *App) KillPortOccupant(port, pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid: %d", pid)
	}
	if pid == os.Getpid() {
		return fmt.Errorf("refusing to terminate the current process")
	}
	current := a.GetPortOccupant(port)
	if current == nil || current.PID <= 0 || current.PID != pid {
		return fmt.Errorf("process %d is no longer listening on port %d; refresh and retry", pid, port)
	}
	name, _ := proxy.GetProcessNameByPID(pid)
	if self := ownExeName(); self != "" && strings.EqualFold(strings.TrimSuffix(name, ".exe"), self) {
		return fmt.Errorf("refusing to terminate %s: it is this application", name)
	}
	return proxy.KillProcessByPID(pid)
}
