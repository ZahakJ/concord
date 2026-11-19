//go:build !wails && linux

package main

import (
	"net"
	"syscall"
)

// launchPeerListener wraps the launch socket's listener so that it only ever
// hands the server a connection whose peer runs as uid (SO_PEERCRED). The
// socket's directory and mode already keep other users out; this keeps them
// out even if those are ever wrong.
func launchPeerListener(ln net.Listener, uid int) net.Listener {
	return peerUIDListener{Listener: ln, uid: uid}
}

type peerUIDListener struct {
	net.Listener
	uid int
}

func (l peerUIDListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if uid, ok := peerUID(c); ok && uid == l.uid {
			return c, nil
		}
		c.Close()
	}
}

// peerUID is the uid of the process at the other end of a unix connection.
func peerUID(c net.Conn) (int, bool) {
	uc, ok := c.(*net.UnixConn)
	if !ok {
		return 0, false
	}
	raw, err := uc.SyscallConn()
	if err != nil {
		return 0, false
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil || credErr != nil || cred == nil {
		return 0, false
	}
	return int(cred.Uid), true
}
