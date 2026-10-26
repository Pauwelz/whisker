package media_test

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/Pauwelz/whisker/daemon/internal/media"
)

func seg(m byte, payload string) []byte {
	b := []byte{0xFF, m, 0, 0}
	binary.BigEndian.PutUint16(b[2:], uint16(len(payload)+2))
	return append(b, payload...)
}

// exifWith builds a little-endian Exif payload with Orientation and a GPS pointer tag.
func exifWith(orientation byte) string {
	return "Exif\x00\x00II*\x00\x08\x00\x00\x00\x02\x00" +
		"\x12\x01\x03\x00\x01\x00\x00\x00" + string([]byte{orientation, 0, 0, 0}) +
		"\x25\x88\x04\x00\x01\x00\x00\x00\x00\x00\x00\x00" + "\x00\x00\x00\x00"
}

func realJPEG(t *testing.T) []byte {
	var buf bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{200, 10, 10, 255})
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withSegments inserts segments after the SOI of a real JPEG.
func withSegments(j []byte, segs ...[]byte) []byte {
	return append(append(append([]byte{}, j[:2]...), bytes.Join(segs, nil)...), j[2:]...)
}

func TestStripJPEGDropsMetadataAndKeepsTheRest(t *testing.T) {
	j := withSegments(realJPEG(t), seg(0xE0, "JFIF\x00"), seg(0xE1, "Exif\x00\x00GPS"), seg(0xE1, "http://ns.adobe.com/xap/1.0/\x00<x/>"),
		seg(0xE2, "ICC_PROFILE\x00"), seg(0xED, "Photoshop 3.0\x00"), seg(0xFE, "comment"))
	out, err := media.StripMetadata("image/jpeg", j)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"Exif", "GPS", "adobe", "Photoshop", "comment"} {
		if bytes.Contains(out, []byte(gone)) {
			t.Errorf("output still holds %q", gone)
		}
	}
	for _, kept := range []string{"JFIF", "ICC_PROFILE"} {
		if !bytes.Contains(out, []byte(kept)) {
			t.Errorf("output lost %q", kept)
		}
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("decode: %v", err)
	}
}

func TestStripJPEGKeepsOnlyTheOrientation(t *testing.T) {
	j := withSegments(realJPEG(t), seg(0xE0, "JFIF\x00"), seg(0xE1, exifWith(6)))
	out, err := media.StripMetadata("image/jpeg", j)
	if err != nil {
		t.Fatal(err)
	}
	want := []byte("Exif\x00\x00MM\x00*\x00\x00\x00\x08\x00\x01\x01\x12\x00\x03\x00\x00\x00\x01\x00\x06\x00\x00\x00\x00\x00\x00")
	if !bytes.Contains(out, want) {
		t.Fatalf("minimal Exif missing in %x", out[:60])
	}
	if bytes.Index(out, []byte("JFIF")) > bytes.Index(out, []byte("Exif")) {
		t.Error("Exif must come after JFIF")
	}
	if _, err := jpeg.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("decode: %v", err)
	}
}

func pngChunk(typ, data string) []byte {
	b := make([]byte, 4, 12+len(data))
	binary.BigEndian.PutUint32(b, uint32(len(data)))
	b = append(b, typ+data...)
	return binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE([]byte(typ+data)))
}

func TestStripPNGDropsMetadataChunks(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	p := buf.Bytes()
	ihdrEnd := 8 + 25
	extra := bytes.Join([][]byte{pngChunk("eXIf", "GPS"), pngChunk("tEXt", "k\x00v"), pngChunk("zTXt", "z"), pngChunk("iTXt", "i"), pngChunk("tIME", "1234567"), pngChunk("gAMA", "1234")}, nil)
	in := append(append(append([]byte{}, p[:ihdrEnd]...), extra...), p[ihdrEnd:]...)
	out, err := media.StripMetadata("image/png", in)
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"eXIf", "tEXt", "zTXt", "iTXt", "tIME"} {
		if bytes.Contains(out, []byte(gone)) {
			t.Errorf("output still holds %s", gone)
		}
	}
	if !bytes.Contains(out, []byte("gAMA")) {
		t.Error("gAMA was dropped")
	}
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Errorf("decode: %v", err)
	}
}

func riffChunk(fourcc, data string) []byte {
	b := binary.LittleEndian.AppendUint32([]byte(fourcc), uint32(len(data)))
	b = append(b, data...)
	if len(data)%2 == 1 {
		b = append(b, 0)
	}
	return b
}

func webp(chunks ...[]byte) []byte {
	body := append([]byte("WEBP"), bytes.Join(chunks, nil)...)
	return append(binary.LittleEndian.AppendUint32([]byte("RIFF"), uint32(len(body))), body...)
}

func TestStripWebPDropsExifAndXMPAndFixesTheSize(t *testing.T) {
	vp8x := "\x2C\x00\x00\x00\x03\x00\x00\x03\x00\x00" // ICC, Exif, XMP, alpha flags
	in := webp(riffChunk("VP8X", vp8x), riffChunk("EXIF", "GPS12"), riffChunk("XMP ", "<x/>"), riffChunk("VP8L", "pixels!"))
	out, err := media.StripMetadata("image/webp", in)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte("EXIF")) || bytes.Contains(out, []byte("XMP ")) {
		t.Error("output still holds a metadata chunk")
	}
	if flags := out[20]; flags != 0x20 {
		t.Errorf("VP8X flags = %#x, want 0x20", flags)
	}
	if got := int(binary.LittleEndian.Uint32(out[4:])); got != len(out)-8 {
		t.Errorf("RIFF size = %d, want %d", got, len(out)-8)
	}
	if !bytes.Contains(out, []byte("pixels!")) {
		t.Error("pixel chunk lost")
	}
}

func TestStripLeavesAFileWithoutMetadataUnchanged(t *testing.T) {
	var pb bytes.Buffer
	png.Encode(&pb, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	for mime, in := range map[string][]byte{
		"image/jpeg": realJPEG(t),
		"image/png":  pb.Bytes(),
		"image/webp": webp(riffChunk("VP8L", "pixels!")),
	} {
		out, err := media.StripMetadata(mime, in)
		if err != nil || !bytes.Equal(out, in) {
			t.Errorf("%s: err = %v, equal = %v", mime, err, bytes.Equal(out, in))
		}
	}
}

func TestStripRefusesBrokenFiles(t *testing.T) {
	j := realJPEG(t)
	w := webp(riffChunk("VP8L", "pixels!"))
	for name, c := range map[string]struct {
		mime string
		in   []byte
	}{
		"jpeg truncated in a segment": {"image/jpeg", j[:10]},
		"jpeg without a scan":         {"image/jpeg", withSegments([]byte{0xFF, 0xD8}, seg(0xE0, "JFIF\x00"))},
		"jpeg of text":                {"image/jpeg", []byte("not an image")},
		"png truncated":               {"image/png", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR")},
		"png of a jpeg":               {"image/png", j},
		"webp truncated":              {"image/webp", w[:len(w)-3]},
		"webp of a jpeg":              {"image/webp", j},
		"other type":                  {"image/gif", []byte("GIF89a")},
	} {
		if _, err := media.StripMetadata(c.mime, c.in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
