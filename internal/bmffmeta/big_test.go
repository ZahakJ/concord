package bmffmeta

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// bigVideo writes a moov-first video of about size bytes (256 KiB picture
// chunks with a 64-byte GPS chunk after each), cleans it file to file, and
// logs the time and the bytes allocated.
func bigVideo(t *testing.T, size int) {
	dir := t.TempDir()
	const chunk = 256 << 10
	n := size / (chunk + 64)
	var vOffs, gOffs []byte
	buildMoov := func(start int) []byte {
		vOffs, gOffs = nil, nil
		at := start
		for i := 0; i < n; i++ {
			vOffs = append(vOffs, u32(at)...)
			at += chunk
			gOffs = append(gOffs, u32(at)...)
			at += 64
		}
		trak := func(h string, offs []byte, sz int) []byte {
			return bx("trak",
				fb("tkhd", 0, 3, make([]byte, 80)),
				bx("mdia", fb("mdhd", 0, 0, make([]byte, 20)),
					fb("hdlr", 0, 0, u32(0), []byte(h), make([]byte, 12), []byte("\x00")),
					bx("minf", bx("dinf", fb("dref", 0, 0, u32(1), fb("url ", 0, 1))),
						bx("stbl", fb("stsc", 0, 0, u32(1), u32(1), u32(1), u32(1)),
							fb("stsz", 0, 0, u32(sz), u32(n)),
							fb("stco", 0, 0, u32(n), offs)))))
		}
		return bx("moov", fb("mvhd", 0, 0, make([]byte, 96)), trak("vide", vOffs, chunk), trak("meta", gOffs, 64),
			bx("udta", bx("\xa9xyz", []byte("SECRET-XYZ"))))
	}
	ftyp := bx("ftyp", []byte("isom"), u32(0), []byte("isom"))
	m := buildMoov(0)
	m = buildMoov(len(ftyp) + len(m) + 8)
	inPath := filepath.Join(dir, "in.mp4")
	f, err := os.Create(inPath)
	if err != nil {
		t.Fatal(err)
	}
	w := bufio.NewWriterSize(f, 1<<20)
	w.Write(ftyp)
	w.Write(m)
	w.Write(u32(8 + n*(chunk+64)))
	w.Write([]byte("mdat"))
	vc := bytes.Repeat([]byte("V"), chunk)
	gc := pad("GPS-SECRET", 64)
	for i := 0; i < n; i++ {
		w.Write(vc)
		w.Write(gc)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	in, _ := os.Open(inPath)
	defer in.Close()
	st, _ := in.Stat()
	out, _ := os.Create(filepath.Join(dir, "out.mp4"))
	defer out.Close()
	runtime.GC()
	var a, b runtime.MemStats
	runtime.ReadMemStats(&a)
	t0 := time.Now()
	if _, err := Strip(in, st.Size(), out); err != nil {
		t.Fatal(err)
	}
	el := time.Since(t0)
	runtime.ReadMemStats(&b)
	os2, _ := out.Stat()
	t.Logf("input %d bytes (%d chunks, moov %d bytes) -> output %d bytes in %v; allocated %d bytes",
		st.Size(), n, len(m), os2.Size(), el, b.TotalAlloc-a.TotalAlloc)
	if alloc := b.TotalAlloc - a.TotalAlloc; alloc > 4<<20 {
		t.Errorf("allocated %d bytes for a %d-byte file", alloc, st.Size())
	}
	// The GPS chunks are zeros now; spot-check the last one.
	ob, _ := os.ReadFile(filepath.Join(dir, "out.mp4"))
	if bytes.Contains(ob, []byte("GPS-SECRET")) || bytes.Contains(ob, []byte("SECRET-XYZ")) {
		t.Error("secrets survived")
	}
}

// TestWriteCleanedFixtures writes each cleaned fixture into $BMFF_OUT, for a
// check by tools outside Go (ffmpeg -f framemd5, heif-dec).
func TestWriteCleanedFixtures(t *testing.T) {
	dir := os.Getenv("BMFF_OUT")
	if dir == "" {
		t.Skip("BMFF_OUT not set")
	}
	for _, n := range []string{"android-fast.mp4", "android-tail.mp4", "iphone-fast.mov", "iphone-tail.mov", "photo.heic"} {
		in, err := os.ReadFile(filepath.Join("testdata", n))
		if err != nil {
			t.Fatal(err)
		}
		out, _, err := StripBytes(in)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "clean-"+n), out, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
