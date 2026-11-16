package imgmeta

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"math/rand"
	"testing"

	"github.com/ZahakJ/concord/internal/imgmeta/imgfixture"
)

func TestStripRemovesMetadataAndKeepsThePicture(t *testing.T) {
	for _, f := range imgfixture.All() {
		t.Run(f.Name, func(t *testing.T) {
			if err := f.Check(f.Bytes); err == nil {
				t.Fatal("the fixture passes the check before cleaning; the check proves nothing")
			}
			out, err := Strip(f.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Check(out); err != nil {
				t.Fatal(err)
			}
			again, err := Strip(out)
			if err != nil || !bytes.Equal(again, out) {
				t.Fatalf("cleaning a clean picture changed it (err %v)", err)
			}
		})
	}
}

// A picture already upright needs no EXIF at all, so none is written.
func TestUprightJPEGCarriesNoExif(t *testing.T) {
	var b bytes.Buffer
	if err := jpeg.Encode(&b, image.NewGray(image.Rect(0, 0, 8, 8)), nil); err != nil {
		t.Fatal(err)
	}
	in := append([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0, 34}, "Exif\x00\x00"...)
	in = append(in, orientationTIFF(1)...)
	in = append(in, b.Bytes()[2:]...)
	out, err := Strip(in)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, b.Bytes()) {
		t.Fatal("an upright picture kept an EXIF block, or lost a byte of its own")
	}
}

// The format comes from the bytes: a JPEG labelled PNG is still cleaned.
func TestFormatIsReadFromTheBytes(t *testing.T) {
	if Format(imgfixture.JPEG()) != "jpeg" || Format(imgfixture.PNG()) != "png" ||
		Format(imgfixture.GIF()) != "gif" || Format(imgfixture.WebP()) != "webp" {
		t.Fatal("a fixture was not recognised")
	}
	heic := append([]byte{0, 0, 0, 24}, "ftypheic"...)
	heic = append(heic, make([]byte, 16)...)
	if _, err := Strip(heic); err != ErrUnsupported {
		t.Fatalf("HEIC: err %v, want ErrUnsupported", err)
	}
	junk := []byte("not a picture at all")
	if out, err := Strip(junk); err != nil || !bytes.Equal(out, junk) {
		t.Fatalf("unknown bytes: %q, %v", out, err)
	}
}

// A file cut short still comes out without its metadata, and what remains of
// the picture is kept.
func TestTruncatedPicturesAreStillCleaned(t *testing.T) {
	for _, f := range imgfixture.All() {
		for _, cut := range []int{len(f.Bytes) / 2, len(f.Bytes) - 30} {
			out, err := Strip(f.Bytes[:cut])
			if err != nil {
				t.Fatalf("%s cut at %d: %v", f.Name, cut, err)
			}
			for _, s := range []string{imgfixture.SecretGPS, imgfixture.SecretXMP, imgfixture.SecretMakerNote, imgfixture.SecretComment} {
				if bytes.Contains(out, []byte(s)) {
					t.Fatalf("%s cut at %d: %q survived", f.Name, cut, s)
				}
			}
		}
	}
}

// bigJPEG is a ~5 MB phone-style JPEG: noise at high quality, with the
// fixture's metadata in front.
func bigJPEG(tb testing.TB) []byte {
	tb.Helper()
	r := rand.New(rand.NewSource(1))
	img := image.NewRGBA(image.Rect(0, 0, 2000, 2560))
	for i := range img.Pix {
		img.Pix[i] = uint8(r.Intn(256))
	}
	for y := 0; y < 2560; y++ {
		img.Set(0, y, color.White)
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, &jpeg.Options{Quality: 92}); err != nil {
		tb.Fatal(err)
	}
	head := imgfixture.JPEG()
	sos := bytes.Index(head, []byte{0xFF, 0xDB}) // first table: end of the metadata
	return append(append([]byte{}, head[:sos]...), b.Bytes()[2:]...)
}

func BenchmarkStrip5MBJPEG(b *testing.B) {
	in := bigJPEG(b)
	b.SetBytes(int64(len(in)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := Strip(in); err != nil {
			b.Fatal(err)
		}
	}
}

func TestBenchInputIsFiveMegabytes(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 5 MB picture")
	}
	if n := len(bigJPEG(t)); n < 4<<20 || n > 5<<20 {
		t.Fatalf("benchmark input is %d bytes, want about 5 MB", n)
	}
}
