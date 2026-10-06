package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"snishaper/pkg/subscription"
)

type nodeSpec struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Server  string            `json:"server"`
	Port    int               `json:"port"`
	Options map[string]string `json:"options"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Println("usage: roundtrip <nodes.json>")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Println("read:", err)
		os.Exit(1)
	}
	var specs []nodeSpec
	if err := json.Unmarshal(data, &specs); err != nil {
		fmt.Println("parse:", err)
		os.Exit(1)
	}

	failed := 0
	for _, spec := range specs {
		fmt.Printf("dialing %s (%s:%d)...\n", spec.Name, spec.Server, spec.Port)
		if err := roundtrip(spec); err != nil {
			fmt.Printf("FAIL %s: %v\n", spec.Name, err)
			failed++
			continue
		}
		fmt.Printf("OK %s\n", spec.Name)
	}
	os.Exit(map[bool]int{true: 0, false: 1}[failed == 0])
}

func roundtrip(spec nodeSpec) error {
	done := make(chan net.Conn, 1)
	errCh := make(chan error, 1)
	go func() {
		node := subscription.Node{
			Name:    spec.Name,
			Type:    spec.Type,
			Server:  spec.Server,
			Port:    spec.Port,
			Options: spec.Options,
		}
		conn, err := node.DialTimeout("echo.fake.test", 9999, 10*time.Second)
		if err != nil {
			errCh <- err
			return
		}
		done <- conn
	}()
	var conn net.Conn
	select {
	case c := <-done:
		conn = c
	case err := <-errCh:
		return fmt.Errorf("dial: %w", err)
	case <-time.After(15 * time.Second):
		return fmt.Errorf("dial timed out after 15s")
	}
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(10 * time.Second)); err != nil {
		return err
	}
	msg := []byte("roundtrip-" + spec.Name + "-0123456789abcdef")
	if _, err := conn.Write(msg); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(conn, got); err != nil {
		return fmt.Errorf("read: %w", err)
	}
	for i := range msg {
		if got[i] != msg[i] {
			return fmt.Errorf("mismatch at %d: got %q", i, got)
		}
	}
	return nil
}
