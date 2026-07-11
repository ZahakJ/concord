// Copyright 2024 The Libc Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build linux && (amd64 || arm64 || loong64 || ppc64le || s390x || riscv64 || 386 || arm)

// PATCHED for Concord (see the android-core Makefile target): on Android,
// the app seccomp filter denies the legacy x86_64 path syscalls
// (open/stat/lstat/mkdir/...) that musl-translated code issues, killing the
// process with SIGSYS the moment sqlite touches a file. androidRemap below
// rewrites those calls to their modern *at equivalents before dispatch. The
// remap is compiled in only for GOARCH=amd64 (the emulator/ChromeOS ABI —
// syscall numbers are arch-specific); arm64 devices never used the legacy
// numbers, so real phones are untouched. Keep this file in sync with
// upstream modernc.org/libc syscall_musl.go when bumping the dependency.

package libc // import "modernc.org/libc"

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Legacy x86_64 syscall numbers remapped by androidRemap, plus their modern
// replacements. Named locally so this file still compiles on the other musl
// arches (whose unix package does not define the legacy constants).
const (
	sysOpen     = 2
	sysStat     = 4
	sysLstat    = 6
	sysAccess   = 21
	sysPipe     = 22
	sysDup2     = 33
	sysRename   = 82
	sysMkdir    = 83
	sysRmdir    = 84
	sysLink     = 86
	sysUnlink   = 87
	sysSymlink  = 88
	sysReadlink = 89
	sysChmod    = 90
	sysChown    = 92
	sysUtime    = 132
	sysMknod    = 133
	sysUtimes   = 235

	sysOpenat     = 257
	sysMkdirat    = 258
	sysMknodat    = 259
	sysFchownat   = 260
	sysNewfstatat = 262
	sysUnlinkat   = 263
	sysRenameat   = 264
	sysLinkat     = 265
	sysSymlinkat  = 266
	sysReadlinkat = 267
	sysFchmodat   = 268
	sysFaccessat  = 269
	sysUtimensat  = 280
	sysDup3       = 292
	sysPipe2      = 293
)

const atFDCWD = ^uintptr(99) // unix.AT_FDCWD (-100) as a raw syscall argument

// androidRemap intercepts legacy path syscalls and performs the modern
// equivalent. It reports handled=false when n needs no translation. The
// GOARCH check is a compile-time constant comparison, so on every arch but
// amd64 this whole function folds away to `return 0, false`.
func androidRemap(n, a1, a2, a3 long) (r long, handled bool) {
	if runtime.GOARCH != "amd64" {
		return 0, false
	}
	var r1 uintptr
	var err unix.Errno
	switch n {
	case sysOpen:
		r1, _, err = unix.Syscall6(sysOpenat, atFDCWD, uintptr(a1), uintptr(a2), uintptr(a3), 0, 0)
	case sysStat:
		r1, _, err = unix.Syscall6(sysNewfstatat, atFDCWD, uintptr(a1), uintptr(a2), 0, 0, 0)
	case sysLstat:
		r1, _, err = unix.Syscall6(sysNewfstatat, atFDCWD, uintptr(a1), uintptr(a2), uintptr(unix.AT_SYMLINK_NOFOLLOW), 0, 0)
	case sysAccess:
		r1, _, err = unix.Syscall6(sysFaccessat, atFDCWD, uintptr(a1), uintptr(a2), 0, 0, 0)
	case sysPipe:
		r1, _, err = unix.Syscall(sysPipe2, uintptr(a1), 0, 0)
	case sysDup2:
		if a1 == a2 {
			// dup3 rejects oldfd == newfd; dup2 returns newfd if oldfd is
			// valid. F_GETFD probes validity without side effects.
			if _, _, e := unix.Syscall(unix.SYS_FCNTL, uintptr(a1), unix.F_GETFD, 0); e != 0 {
				return long(-e), true
			}
			return a2, true
		}
		r1, _, err = unix.Syscall(sysDup3, uintptr(a1), uintptr(a2), 0)
	case sysRename:
		r1, _, err = unix.Syscall6(sysRenameat, atFDCWD, uintptr(a1), atFDCWD, uintptr(a2), 0, 0)
	case sysMkdir:
		r1, _, err = unix.Syscall(sysMkdirat, atFDCWD, uintptr(a1), uintptr(a2))
	case sysRmdir:
		r1, _, err = unix.Syscall(sysUnlinkat, atFDCWD, uintptr(a1), uintptr(unix.AT_REMOVEDIR))
	case sysLink:
		r1, _, err = unix.Syscall6(sysLinkat, atFDCWD, uintptr(a1), atFDCWD, uintptr(a2), 0, 0)
	case sysUnlink:
		r1, _, err = unix.Syscall(sysUnlinkat, atFDCWD, uintptr(a1), 0)
	case sysSymlink:
		r1, _, err = unix.Syscall(sysSymlinkat, uintptr(a1), atFDCWD, uintptr(a2))
	case sysReadlink:
		r1, _, err = unix.Syscall6(sysReadlinkat, atFDCWD, uintptr(a1), uintptr(a2), uintptr(a3), 0, 0)
	case sysChmod:
		r1, _, err = unix.Syscall(sysFchmodat, atFDCWD, uintptr(a1), uintptr(a2))
	case sysChown:
		r1, _, err = unix.Syscall6(sysFchownat, atFDCWD, uintptr(a1), uintptr(a2), uintptr(a3), 0, 0)
	case sysMknod:
		r1, _, err = unix.Syscall6(sysMknodat, atFDCWD, uintptr(a1), uintptr(a2), uintptr(a3), 0, 0)
	case sysUtime:
		// utime(path, *utimbuf{actime, modtime int64}) -> utimensat.
		var tsp uintptr
		var ts [2]unix.Timespec
		if a2 != 0 {
			ub := (*[2]int64)(unsafe.Pointer(uintptr(a2)))
			// NsecToTimespec keeps this portable across the 32-bit ABIs,
			// where Timespec fields are int32.
			ts[0] = unix.NsecToTimespec(ub[0] * 1e9)
			ts[1] = unix.NsecToTimespec(ub[1] * 1e9)
			tsp = uintptr(unsafe.Pointer(&ts))
		}
		r1, _, err = unix.Syscall6(sysUtimensat, atFDCWD, uintptr(a1), tsp, 0, 0, 0)
	case sysUtimes:
		// utimes(path, *timeval[2]{sec, usec int64}) -> utimensat.
		var tsp uintptr
		var ts [2]unix.Timespec
		if a2 != 0 {
			tv := (*[4]int64)(unsafe.Pointer(uintptr(a2)))
			ts[0] = unix.NsecToTimespec(tv[0]*1e9 + tv[1]*1000)
			ts[1] = unix.NsecToTimespec(tv[2]*1e9 + tv[3]*1000)
			tsp = uintptr(unsafe.Pointer(&ts))
		}
		r1, _, err = unix.Syscall6(sysUtimensat, atFDCWD, uintptr(a1), tsp, 0, 0, 0)
	default:
		return 0, false
	}
	if err != 0 {
		return long(-err), true
	}
	return long(r1), true
}

func ___syscall_cp(tls *TLS, n, a, b, c, d, e, f long) long {
	if r, ok := androidRemap(n, a, b, c); ok {
		return r
	}
	r1, _, err := (unix.Syscall6(uintptr(n), uintptr(a), uintptr(b), uintptr(c), uintptr(d), uintptr(e), uintptr(f)))
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall0(tls *TLS, n long) long {
	switch n {
	case __NR_sched_yield:
		runtime.Gosched()
		return 0
	default:
		r1, _, err := unix.Syscall(uintptr(n), 0, 0, 0)
		if err != 0 {
			return long(-err)
		}

		return long(r1)
	}
}

func X__syscall1(tls *TLS, n, a1 long) long {
	if r, ok := androidRemap(n, a1, 0, 0); ok {
		return r
	}
	r1, _, err := unix.Syscall(uintptr(n), uintptr(a1), 0, 0)
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall2(tls *TLS, n, a1, a2 long) long {
	if r, ok := androidRemap(n, a1, a2, 0); ok {
		return r
	}
	r1, _, err := unix.Syscall(uintptr(n), uintptr(a1), uintptr(a2), 0)
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall3(tls *TLS, n, a1, a2, a3 long) long {
	if r, ok := androidRemap(n, a1, a2, a3); ok {
		return r
	}
	r1, _, err := unix.Syscall(uintptr(n), uintptr(a1), uintptr(a2), uintptr(a3))
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall4(tls *TLS, n, a1, a2, a3, a4 long) long {
	if r, ok := androidRemap(n, a1, a2, a3); ok {
		return r
	}
	r1, _, err := unix.Syscall6(uintptr(n), uintptr(a1), uintptr(a2), uintptr(a3), uintptr(a4), 0, 0)
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall5(tls *TLS, n, a1, a2, a3, a4, a5 long) long {
	if r, ok := androidRemap(n, a1, a2, a3); ok {
		return r
	}
	r1, _, err := unix.Syscall6(uintptr(n), uintptr(a1), uintptr(a2), uintptr(a3), uintptr(a4), uintptr(a5), 0)
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}

func X__syscall6(tls *TLS, n, a1, a2, a3, a4, a5, a6 long) long {
	if r, ok := androidRemap(n, a1, a2, a3); ok {
		return r
	}
	r1, _, err := unix.Syscall6(uintptr(n), uintptr(a1), uintptr(a2), uintptr(a3), uintptr(a4), uintptr(a5), uintptr(a6))
	if err != 0 {
		return long(-err)
	}

	return long(r1)
}
