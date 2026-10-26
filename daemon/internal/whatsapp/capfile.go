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
