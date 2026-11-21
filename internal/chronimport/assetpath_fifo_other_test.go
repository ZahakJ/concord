//go:build !unix

package chronimport

func mkfifo(string) bool { return false }
