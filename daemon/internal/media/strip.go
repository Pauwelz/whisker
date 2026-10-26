package media

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// ErrBadImage means the bytes do not parse as the image type they claim.
var ErrBadImage = errors.New("not a valid image")

// StripMetadata removes the metadata of an image without a re-encode, so pixels stay as they are.
// JPEG keeps its JFIF and ICC segments and gets a minimal Exif with only the Orientation of the original.
// PNG and WebP lose their Exif, XMP and text chunks. Bytes after the end of the image are dropped.
// A file that does not parse as its type gives ErrBadImage.
func StripMetadata(mime string, b []byte) ([]byte, error) {
	var out []byte
	switch mime {
	case "image/jpeg":
		out = stripJPEG(b)
	case "image/png":
		out = stripPNG(b)
	case "image/webp":
		out = stripWebP(b)
	}
	if out == nil {
		return nil, ErrBadImage
	}
	return out, nil
}

// stripJPEG walks the segments up to the scan and copies the rest as it is. It gives nil for a broken file.
func stripJPEG(b []byte) []byte {
	if len(b) < 4 || b[0] != 0xFF || b[1] != 0xD8 {
		return nil
	}
	out := append(make([]byte, 0, len(b)), b[:2]...)
	insertAt, orientation := 2, 0
	for i := 2; i < len(b); {
		if b[i] != 0xFF {
			return nil
		}
		if i+1 >= len(b) {
			return nil
		}
		m := b[i+1]
		if m == 0xFF { // fill byte
			i++
			continue
		}
		if m == 0x01 || m >= 0xD0 && m <= 0xD8 { // markers without a length
			out = append(out, 0xFF, m)
			i += 2
			continue
		}
		if i+4 > len(b) {
			return nil
		}
		end := i + 2 + int(binary.BigEndian.Uint16(b[i+2:]))
		if end > len(b) || end < i+4 {
			return nil
		}
		seg := b[i:end]
		switch {
		case m == 0xE1 || m == 0xED || m == 0xFE: // APP1 (Exif, XMP), APP13 (IPTC), COM
			if m == 0xE1 && orientation == 0 {
				orientation = exifOrientation(seg[4:])
			}
		case m == 0xDA: // the scan data follows and runs to the end
			out = append(out, b[i:]...)
			if orientation == 0 {
				return out
			}
			return append(out[:insertAt:insertAt], append(minimalExif(orientation), out[insertAt:]...)...)
		default:
			out = append(out, seg...)
			if m == 0xE0 && insertAt == len(out)-len(seg) {
				insertAt = len(out)
			}
		}
		i = end
	}
	return nil
}

// exifOrientation reads tag 0x0112 from the first IFD of an Exif payload. It gives 0 when there is none.
func exifOrientation(p []byte) int {
	if !bytes.HasPrefix(p, []byte("Exif\x00\x00")) || len(p) < 14 {
		return 0
	}
	t := p[6:]
	var o binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		o = binary.LittleEndian
	case "MM":
		o = binary.BigEndian
	default:
		return 0
	}
	ifd := int(o.Uint32(t[4:]))
	if ifd < 8 || ifd+2 > len(t) {
		return 0
	}
	n := int(o.Uint16(t[ifd:]))
	for e := ifd + 2; n > 0 && e+12 <= len(t); n, e = n-1, e+12 {
		if o.Uint16(t[e:]) == 0x0112 && o.Uint16(t[e+2:]) == 3 && o.Uint32(t[e+4:]) == 1 {
			if v := int(o.Uint16(t[e+8:])); v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 0
}

// minimalExif is an APP1 segment with one big-endian IFD that holds only the Orientation.
func minimalExif(orientation int) []byte {
	return []byte{
		0xFF, 0xE1, 0, 34, 'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0, 0x2A, 0, 0, 0, 8,
		0, 1, 0x01, 0x12, 0, 3, 0, 0, 0, 1, 0, byte(orientation), 0, 0,
		0, 0, 0, 0,
	}
}

// stripPNG drops the metadata chunks and stops after IEND. It gives nil for a broken file.
func stripPNG(b []byte) []byte {
	const sig = "\x89PNG\r\n\x1a\n"
	if !bytes.HasPrefix(b, []byte(sig)) {
		return nil
	}
	out := append(make([]byte, 0, len(b)), sig...)
	for i := len(sig); i+12 <= len(b); {
		end := i + 12 + int(binary.BigEndian.Uint32(b[i:]))
		if end > len(b) || end < i+12 {
			return nil
		}
		typ := string(b[i+4 : i+8])
		switch typ {
		case "eXIf", "tEXt", "zTXt", "iTXt", "tIME":
		default:
			out = append(out, b[i:end]...)
		}
		if typ == "IEND" {
			return out
		}
		i = end
	}
	return nil
}

// stripWebP drops the EXIF and XMP chunks, clears their VP8X flags and fixes the RIFF size. It gives nil for a broken file.
func stripWebP(b []byte) []byte {
	if len(b) < 20 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return nil
	}
	size := int(binary.LittleEndian.Uint32(b[4:]))
	if size < 12 || size+8 > len(b) {
		return nil
	}
	b = b[:size+8]
	out := append(make([]byte, 0, len(b)), b[:12]...)
	for i := 12; i < len(b); {
		if i+8 > len(b) {
			return nil
		}
		n := int(binary.LittleEndian.Uint32(b[i+4:]))
		end := i + 8 + n + n&1
		if end > len(b) || end < i+8 {
			return nil
		}
		switch string(b[i : i+4]) {
		case "EXIF", "XMP ":
		case "VP8X":
			if n < 10 {
				return nil
			}
			at := len(out)
			out = append(out, b[i:end]...)
			out[at+8] &^= 0x08 | 0x04
		default:
			out = append(out, b[i:end]...)
		}
		i = end
	}
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	return out
}
