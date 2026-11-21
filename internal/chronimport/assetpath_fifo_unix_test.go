//go:build unix

package chronimport

import "golang.org/x/sys/unix"

func mkfifo(p string) bool { return unix.Mkfifo(p, 0o600) == nil }
