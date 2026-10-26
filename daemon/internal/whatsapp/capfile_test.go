package whatsapp

import (
	"bytes"
	"errors"
	"io"
	"math"
	"os"
	"runtime"
	"testing"
)

// The File that DownloadThumbnail gives to whatsmeow fails past the cap and keeps memory near it,
// however large the body. A whole whatsmeow Client needs a live connection, so this tests the File on its own.
func TestCapFileStopsAtTheCap(t *testing.T) {
	f := &capFile{}
	body := io.LimitReader(zeros{}, 512<<20) // a body of 512 MiB
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	n, err := io.Copy(f, body)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, ErrThumbnailTooLarge) {
		t.Fatalf("copy = %v, want ErrThumbnailTooLarge", err)
	}
	if n > MaxThumbnailBytes {
		t.Errorf("wrote %d bytes, want at most %d", n, MaxThumbnailBytes)
	}
	if grown := after.TotalAlloc - before.TotalAlloc; grown > 8*MaxThumbnailBytes {
		t.Errorf("allocated %d bytes, want far below the 512 MiB body, near the cap %d", grown, MaxThumbnailBytes)
	}
}

func TestCapFileHoldsTheCapAndTruncates(t *testing.T) {
	f := &capFile{}
	if _, err := f.Write(bytes.Repeat([]byte{1}, MaxThumbnailBytes)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte{1}); !errors.Is(err, ErrThumbnailTooLarge) {
		t.Errorf("write past the cap = %v, want ErrThumbnailTooLarge", err)
	}
	if err := f.Truncate(3); err != nil || len(f.buf) != 3 {
		t.Errorf("truncate: %v, len %d", err, len(f.buf))
	}
	f.Seek(0, io.SeekStart)
	if got, _ := io.ReadAll(f); !bytes.Equal(got, []byte{1, 1, 1}) {
		t.Errorf("read back %v", got)
	}
}

type zeros struct{}

func (zeros) Read(p []byte) (int, error) { clear(p); return len(p), nil }

func TestCapDiskFailsPastTheCapAndDropsTheBytes(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "dl")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	f := capDisk{file: file, max: 8}
	if n, err := io.Copy(f, io.LimitReader(zeros{}, 8)); err != nil || n != 8 {
		t.Fatalf("copy up to the cap = %d, %v", n, err)
	}
	if _, err := io.Copy(f, io.LimitReader(zeros{}, 16)); !errors.Is(err, ErrDownloadTooLarge) { // io.Copy must not skip the cap
		t.Fatalf("copy past the cap = %v, want ErrDownloadTooLarge", err)
	}
	if _, err := f.Write([]byte{1}); !errors.Is(err, ErrDownloadTooLarge) {
		t.Fatalf("write past the cap = %v, want ErrDownloadTooLarge", err)
	}
	if _, err := f.WriteAt([]byte{1}, 8); !errors.Is(err, ErrDownloadTooLarge) {
		t.Errorf("WriteAt past the cap = %v, want ErrDownloadTooLarge", err)
	}
	if err := f.Truncate(9); !errors.Is(err, ErrDownloadTooLarge) {
		t.Errorf("Truncate past the cap = %v, want ErrDownloadTooLarge", err)
	}
	if info, _ := file.Stat(); info.Size() != 0 {
		t.Errorf("partial file has %d bytes, want 0", info.Size())
	}
}

func TestDownloadCap(t *testing.T) {
	for size, want := range map[int64]int64{0: MaxDownloadBytes, -1: MaxDownloadBytes, 100: 126, MaxDownloadBytes: MaxDownloadBytes, math.MaxInt64: MaxDownloadBytes} {
		if got := DownloadCap(size); got != want {
			t.Errorf("DownloadCap(%d) = %d, want %d", size, got, want)
		}
	}
}
