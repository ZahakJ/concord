//go:build !linux || android

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "concord-blecheck needs Linux with BlueZ")
	os.Exit(2)
}
