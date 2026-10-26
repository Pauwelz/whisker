package whatsapp

import (
	"errors"
	"io"
	"os"
)

// capFile is an in-memory whatsmeow.File that refuses to grow past MaxThumbnailBytes.
// A write that would pass the cap fails with ErrThumbnailTooLarge and stores nothing.
type capFile struct {
	buf []byte
	pos int64
}

func (f *capFile) Read(p []byte) (int, error) {
	n, err := f.ReadAt(p, f.pos)
	f.pos += int64(n)
	return n, err
}

func (f *capFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.buf)) {
		return 0, io.EOF
	}
	n := copy(p, f.buf[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (f *capFile) Write(p []byte) (int, error) {
	n, err := f.WriteAt(p, f.pos)
	f.pos += int64(n)
	return n, err
}

func (f *capFile) WriteAt(p []byte, off int64) (int, error) {
	end := off + int64(len(p))
	if off < 0 || end > MaxThumbnailBytes {
		return 0, ErrThumbnailTooLarge
	}
	if grow := end - int64(len(f.buf)); grow > 0 {
		f.buf = append(f.buf, make([]byte, grow)...)
	}
	return copy(f.buf[off:], p), nil
}

func (f *capFile) Seek(offset int64, whence int) (int64, error) {
	switch whence {
	case io.SeekCurrent:
		offset += f.pos
	case io.SeekEnd:
		offset += int64(len(f.buf))
	}
	if offset < 0 {
		return 0, errors.New("negative seek")
	}
	f.pos = offset
	return offset, nil
}

func (f *capFile) Truncate(size int64) error {
	if size < 0 || size > int64(len(f.buf)) {
		return errors.New("truncate out of range")
	}
	f.buf = f.buf[:size]
	return nil
}

// sizeInfo is the only part of os.FileInfo that whatsmeow reads.
type sizeInfo struct {
	os.FileInfo
	size int64
}

func (s sizeInfo) Size() int64 { return s.size }

func (f *capFile) Stat() (os.FileInfo, error) { return sizeInfo{size: int64(len(f.buf))}, nil }

// MaxDownloadBytes is the hard cap of one full Media download.
const MaxDownloadBytes = 2 << 30

// downloadOverhead is the AES padding (up to 16 bytes) and the MAC (10 bytes) on top of the plain size.
const downloadOverhead = 16 + 10

// ErrDownloadTooLarge means a download wrote more than its cap.
var ErrDownloadTooLarge = errors.New("download too large")

// DownloadCap is the most bytes a download of declared size may write: the size plus the overhead.
// An unknown size (0 or less), or one near the hard cap, gets MaxDownloadBytes.
func DownloadCap(size int64) int64 {
	if size <= 0 || size > MaxDownloadBytes-downloadOverhead {
		return MaxDownloadBytes
	}
	return size + downloadOverhead
}

// capDisk is a whatsmeow.File over a temp file that refuses to grow past max bytes.
// A write that passes the cap fails with ErrDownloadTooLarge and empties the file at once.
// Being no *os.File, it also stops whatsmeow from reserving disk space for the server's Content-Length.
// It names its methods one by one: embedding *os.File would promote ReadFrom and WriteString, which io.Copy uses and which skip the cap.
type capDisk struct {
	file *os.File
	max  int64
}

func (f capDisk) Read(p []byte) (int, error)              { return f.file.Read(p) }
func (f capDisk) ReadAt(p []byte, off int64) (int, error) { return f.file.ReadAt(p, off) }
func (f capDisk) Seek(off int64, whence int) (int64, error) {
	return f.file.Seek(off, whence)
}
func (f capDisk) Stat() (os.FileInfo, error) { return f.file.Stat() }

func (f capDisk) Write(p []byte) (int, error) {
	pos, err := f.file.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, err
	}
	return f.write(p, pos, func() (int, error) { return f.file.Write(p) })
}

func (f capDisk) WriteAt(p []byte, off int64) (int, error) {
	return f.write(p, off, func() (int, error) { return f.file.WriteAt(p, off) })
}

func (f capDisk) write(p []byte, off int64, do func() (int, error)) (int, error) {
	if off < 0 || off+int64(len(p)) > f.max {
		return 0, f.tooLarge()
	}
	return do()
}

func (f capDisk) Truncate(size int64) error {
	if size > f.max {
		return f.tooLarge()
	}
	return f.file.Truncate(size)
}

func (f capDisk) tooLarge() error {
	f.file.Truncate(0) // drop the partial bytes now; the caller removes the file
	return ErrDownloadTooLarge
}
