//go:build ignore

// Writes the synthetic chat export that the importer's tests use into a
// directory, so a person (or a browser-driving script) can point the import
// wizard at something real.
//
// Build-tagged `ignore` on purpose: it is a developer tool, not part of the
// app, and this keeps it out of every binary while still being one command to
// run —
//
//	go run scripts/make-export-fixture.go /tmp/export
//
// The fixture itself lives in internal/chronimport/fixture.go, where the Go
// tests share one definition of "what an export looks like". Reusing it here
// rather than inventing a second one means the thing the wizard is driven
// against is the thing the estimator's accuracy was measured on.
package main

import (
	"fmt"
	"os"

	"github.com/ZahakJ/concord/internal/chronimport"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: go run scripts/make-export-fixture.go <dir>")
		os.Exit(2)
	}
	dir := os.Args[1]
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "concord:", err)
		os.Exit(1)
	}
	f, err := chronimport.BuildTestExport(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "concord:", err)
		os.Exit(1)
	}
	fmt.Printf("%s: %q, %d channels, %d messages, %d notices, %d authors\n",
		dir, f.Guild, f.Channels, f.Messages, f.Notices, f.Authors)
}
