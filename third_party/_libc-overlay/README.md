# _libc-overlay

A single patched file from **modernc.org/libc**, the pure-Go C runtime that
`modernc.org/sqlite` compiles against.

- Upstream module: `modernc.org/libc` — https://gitlab.com/cznic/libc
- Forked version: **v1.74.1** (the version pinned as an indirect dependency in
  the root `go.mod`)
- Licence: BSD-3-Clause, © 2017 The Libc Authors. Full text in `LICENSE`,
  copied verbatim from the upstream module.
- File patched: `syscall_musl.go`

This directory is not a Go package and is not built by `go build ./...`; the
leading underscore keeps the Go tool from ever walking into it. It is a
committed copy of one file. The `android-core` Makefile target rsyncs the
whole upstream module out of the module cache into `build/libc-fork/`
(gitignored), drops this file over the top, and points a temporary `replace` at
it for the duration of the gomobile bind. Desktop and server builds never see
it.

## What the patch changes

libc's musl-derived code issues the *legacy* x86_64 path syscalls — `open`,
`stat`, `lstat`, `mkdir`, `rename`, `unlink` and friends. Android's app seccomp
filter denies those numbers outright, so on an x86_64 emulator (and on ChromeOS)
the process takes a SIGSYS and dies the instant sqlite touches a file.

The patch adds `androidRemap`, which translates each legacy number into its
modern `*at` equivalent (`open` → `openat` with `AT_FDCWD`, `stat` →
`newfstatat`, `rmdir` → `unlinkat` with `AT_REMOVEDIR`, and so on) and
dispatches that instead. `utime`/`utimes` additionally convert their argument
structs into the `timespec` pair `utimensat` expects; `dup2` is special-cased
because `dup3` rejects `oldfd == newfd`.

Call sites: a guard at the top of `___syscall_cp` and `X__syscall1` through
`X__syscall6`. Nothing else in the file is touched.

`androidRemap` returns early unless `runtime.GOARCH == "amd64"` — a
compile-time constant comparison, so on every other architecture the function
folds away to `return 0, false`. Syscall numbers are architecture-specific and
arm64 never had the legacy ones, so real arm64 phones run the upstream code
path unchanged.

## Keeping it current

The file is a fork, not a diff, so it silently goes stale. When bumping
`modernc.org/libc`, re-diff this file against the new upstream
`syscall_musl.go` and carry the patch forward.
