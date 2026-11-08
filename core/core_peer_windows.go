//go:build windows

package core

import (
	"log"
	"net"
	"os"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	afInet               = 2
	tcpTableOwnerPidAll  = 5
	errorInsufficientBuf = 122
)

type mibTCPRowOwnerPid struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPid  uint32
}

var (
	iphlpapiDLL             = syscall.NewLazyDLL("iphlpapi.dll")
	getExtendedTcpTableProc = iphlpapiDLL.NewProc("GetExtendedTcpTable")

	rpcListenPort = func() uint16 {
		_, port, err := net.SplitHostPort(coreRPCAddr)
		if err != nil {
			return 0
		}
		value, err := strconv.Atoi(port)
		if err != nil || value < 1 || value > 65535 {
			return 0
		}
		return uint16(value)
	}()

	peerRejectLog sync.Once
)

func tcpOwnerTable() ([]mibTCPRowOwnerPid, error) {
	var size uint32
	ret, _, _ := getExtendedTcpTableProc.Call(0, uintptr(unsafe.Pointer(&size)), 0, afInet, tcpTableOwnerPidAll, 0)
	if ret != 0 && syscall.Errno(ret) != syscall.Errno(errorInsufficientBuf) {
		return nil, syscall.Errno(ret)
	}
	if size == 0 {
		return nil, syscall.EINVAL
	}
	for attempt := 0; attempt < 3; attempt++ {
		buf := make([]byte, size)
		ret, _, _ = getExtendedTcpTableProc.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&size)),
			0, afInet, tcpTableOwnerPidAll, 0,
		)
		if ret == 0 {
			count := *(*uint32)(unsafe.Pointer(&buf[0]))
			rowSize := int(unsafe.Sizeof(mibTCPRowOwnerPid{}))
			raw := buf[4:]
			rows := make([]mibTCPRowOwnerPid, 0, count)
			for i := 0; i < int(count); i++ {
				offset := i * rowSize
				if offset+rowSize > len(raw) {
					break
				}
				rows = append(rows, *(*mibTCPRowOwnerPid)(unsafe.Pointer(&raw[offset])))
			}
			return rows, nil
		}
		if syscall.Errno(ret) != syscall.Errno(errorInsufficientBuf) {
			return nil, syscall.Errno(ret)
		}
	}
	return nil, syscall.EINVAL
}

func hostToNetShort(value uint32) uint16 {
	return uint16(value&0xFF)<<8 | uint16((value>>8)&0xFF)
}

func processImagePath(pid uint32) (string, error) {
	handle, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(handle)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(handle, 0, &buf[0], &size); err != nil {
		return "", err
	}
	return windows.UTF16ToString(buf[:size]), nil
}

func peerProcessAllowed(conn net.Conn) bool {
	remote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return true
	}
	if rpcListenPort == 0 {
		return true
	}
	rows, err := tcpOwnerTable()
	if err != nil {
		return true
	}
	clientPort := uint16(remote.Port)
	var pid uint32
	for _, row := range rows {
		if hostToNetShort(row.LocalPort) == clientPort &&
			hostToNetShort(row.RemotePort) == rpcListenPort &&
			row.OwningPid != 0 {
			pid = row.OwningPid
			break
		}
	}
	if pid == 0 {
		return true
	}
	image, err := processImagePath(pid)
	if err != nil {
		return true
	}
	self, err := os.Executable()
	if err != nil {
		return true
	}
	if sameExecutable(image, self) {
		return true
	}
	peerRejectLog.Do(func() {
		log.Printf("[core] rpc peer rejected: pid=%d image=%q is not this executable", pid, image)
	})
	return false
}
