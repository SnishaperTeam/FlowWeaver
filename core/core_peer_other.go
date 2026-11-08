//go:build !windows

package core

import "net"

func peerProcessAllowed(net.Conn) bool { return true }
