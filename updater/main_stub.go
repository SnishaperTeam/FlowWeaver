//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "FlowWeaver updater supports Windows only")
	os.Exit(1)
}
