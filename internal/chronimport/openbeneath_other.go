//go:build !unix

package chronimport

import (
	"errors"
	"os"
)

// openBeneath without openat: check every component with Lstat, open, and
// require that what was opened is the file that was checked. A directory
// swapped for a link or a junction between the walk and the open is still a
// window here (a junction needs no privilege to make); the re-walk and the
// SameFile check after the open narrow it to a swap and a swap back inside it.
// The unix build closes the window properly.
func openBeneath(root, rel string) (*os.File, error) {
	p, ok := checkBeneath(root, rel)
	if !ok {
		return nil, errors.New("chronimport: asset path refused")
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("chronimport: asset is not a regular file")
	}
	if again, ok := checkBeneath(root, rel); !ok || again != p {
		f.Close()
		return nil, errors.New("chronimport: asset path changed while it was opened")
	}
	if li, err := os.Lstat(p); err != nil || !os.SameFile(fi, li) {
		f.Close()
		return nil, errors.New("chronimport: asset path changed while it was opened")
	}
	return f, nil
}
