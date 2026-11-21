//go:build unix

package chronimport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"
)

// openBeneath opens root/rel for reading, walking rel one component at a time
// with openat and O_NOFOLLOW, so no component can be a symlink at the moment
// it is opened: a directory swapped for a link after localAssetPath checked it
// is an ELOOP or ENOTDIR here, not a read from outside the export. rel is
// relative and cleaned (assetCandidates), so it has no "." or ".." in it.
//
// The last component is opened O_NONBLOCK, so a FIFO put where a file was
// expected cannot hang the import in open; it is then refused by the fstat
// check, and a regular file has the flag cleared before it is handed back.
func openBeneath(root, rel string) (*os.File, error) {
	dfd, err := openatRetry(unix.AT_FDCWD, root, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			unix.Close(dfd)
			return nil, errors.New("chronimport: bad asset path component")
		}
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW
		if i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		} else {
			flags |= unix.O_NONBLOCK | unix.O_NOCTTY
		}
		fd, err := openatRetry(dfd, part, flags)
		unix.Close(dfd)
		if err != nil {
			return nil, err
		}
		dfd = fd
	}
	var st unix.Stat_t
	if err := unix.Fstat(dfd, &st); err != nil || st.Mode&unix.S_IFMT != unix.S_IFREG {
		unix.Close(dfd)
		return nil, errors.New("chronimport: asset is not a regular file")
	}
	if err := unix.SetNonblock(dfd, false); err != nil {
		unix.Close(dfd)
		return nil, err
	}
	return os.NewFile(uintptr(dfd), filepath.Join(root, rel)), nil
}

func openatRetry(dfd int, name string, flags int) (int, error) {
	for {
		fd, err := unix.Openat(dfd, name, flags, 0)
		if err != unix.EINTR {
			return fd, err
		}
	}
}
