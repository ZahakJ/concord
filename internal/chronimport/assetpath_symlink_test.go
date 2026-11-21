package chronimport

import (
	"io"
	"os"
	"path/filepath"
	"testing"
)

// symlinkExport builds an export directory holding a regular file, a
// subdirectory, a file symlink and a directory symlink that both point
// outside, and a directory symlink that points back inside — the shapes an
// archive extractor restores from a zip or tarball a stranger sent.
func symlinkExport(t *testing.T) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "export")
	outside = filepath.Join(base, "outside")
	for _, d := range []string{root, outside, filepath.Join(root, "media")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for p, body := range map[string]string{
		filepath.Join(root, "a.png"):            "png",
		filepath.Join(root, "media", "b c.png"): "png2",
		filepath.Join(outside, "secret.txt"):    "secret",
	} {
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(root, "link.png")); err != nil {
		t.Skipf("this filesystem does not do symlinks: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "d")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "media"), filepath.Join(root, "inner")); err != nil {
		t.Fatal(err)
	}
	return root, outside
}

// TestLocalAssetPathRefusesDirectorySymlinks is the assure finding
// (fuzz/README.md, finding 3): only the last path component was Lstat'ed and
// containment was checked lexically, so a directory symlink inside the export
// ("d" -> outside) let "d/secret.txt" be read from outside it. Symlinks are now
// refused in every component, whichever way they point.
func TestLocalAssetPathRefusesDirectorySymlinks(t *testing.T) {
	root, _ := symlinkExport(t)
	for _, raw := range []string{
		"d/secret.txt",
		"d%2Fsecret.txt",
		"./d/secret.txt",
		"media/../d/secret.txt",
		"link.png",
		"inner/b c.png", // points inside, refused all the same
		"d",
		"media",
	} {
		if p, ok := localAssetPath(root, raw); ok {
			t.Errorf("localAssetPath(%q) accepted %q", raw, p)
		}
		if f, ok := OpenLocalAsset(root, raw); ok {
			t.Errorf("OpenLocalAsset(%q) opened %q", raw, f.Name())
			f.Close()
		}
	}
	for raw, want := range map[string]string{
		"a.png":            "png",
		"media/b%20c.png":  "png2",
		"media/b c.png":    "png2",
		"./media/../a.png": "png",
	} {
		if _, ok := localAssetPath(root, raw); !ok {
			t.Errorf("localAssetPath(%q) refused a file inside the export", raw)
		}
		f, ok := OpenLocalAsset(root, raw)
		if !ok {
			t.Errorf("OpenLocalAsset(%q) refused a file inside the export", raw)
			continue
		}
		got, err := io.ReadAll(f)
		f.Close()
		if err != nil || string(got) != want {
			t.Errorf("OpenLocalAsset(%q) read %q, %v; want %q", raw, got, err, want)
		}
	}
}

// TestLocalAssetRootMayBeASymlink: the export directory the owner picked is
// trusted however it is reached (a home directory on another disk is a
// symlink on this very machine); only what is below it is held to the rule.
func TestLocalAssetRootMayBeASymlink(t *testing.T) {
	root, _ := symlinkExport(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip(err)
	}
	if _, ok := localAssetPath(alias, "media/b c.png"); !ok {
		t.Error("a file was refused because the export root itself is reached through a symlink")
	}
	f, ok := OpenLocalAsset(alias, "media/b c.png")
	if !ok {
		t.Fatal("OpenLocalAsset refused a file under a symlinked root")
	}
	f.Close()
}

// TestOpenLocalAssetRefusesASwappedComponent is the time-of-check race: the
// path is checked, then a directory on it is replaced by a symlink before the
// open. Opening re-walks the path one component at a time without following
// links, so the swap is a refusal, not a read from outside.
func TestOpenLocalAssetRefusesASwappedComponent(t *testing.T) {
	root, outside := symlinkExport(t)
	if err := os.WriteFile(filepath.Join(outside, "b c.png"), []byte("outside"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := localAssetPath(root, "media/b c.png"); !ok {
		t.Fatal("setup: the file was refused before the swap")
	}
	if err := os.Rename(filepath.Join(root, "media"), filepath.Join(root, "media.old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "media")); err != nil {
		t.Fatal(err)
	}
	if f, ok := OpenLocalAsset(root, "media/b c.png"); ok {
		b, _ := io.ReadAll(f)
		f.Close()
		t.Fatalf("opened %q through a swapped-in symlink: read %q", f.Name(), b)
	}
}

// TestOpenLocalAssetRefusesNonRegular: a FIFO would block the import forever on
// open, a directory is not an asset.
func TestOpenLocalAssetRefusesNonRegular(t *testing.T) {
	root, _ := symlinkExport(t)
	if f, ok := OpenLocalAsset(root, "media"); ok {
		f.Close()
		t.Error("a directory was opened as an asset")
	}
	if !mkfifo(filepath.Join(root, "pipe.png")) {
		return
	}
	if f, ok := OpenLocalAsset(root, "pipe.png"); ok {
		f.Close()
		t.Error("a FIFO was opened as an asset")
	}
}
