package contract

import (
	"fmt"
	"io"

	"github.com/Pauwelz/whisker/daemon/internal/whatsapp"
)

// storeOp is the kind of call that failed.
type storeOp string

const (
	storeRead  storeOp = "read"
	storeWrite storeOp = "write"
)

// storeFailed logs a chat store failure and reports whether err is one. It logs no error text: it could hold names.
func storeFailed(w io.Writer, op storeOp, err error) bool {
	if err == nil {
		return false
	}
	fmt.Fprintln(w, "chat store "+string(op)+" failed")
	return true
}

// downloadFailed logs a failed download with the HTTP status when the error holds one. It logs no error text: it could hold hosts.
func downloadFailed(w io.Writer, what string, err error) {
	if status := whatsapp.DownloadStatus(err); status != 0 {
		fmt.Fprintf(w, "%s download failed status=%d\n", what, status)
		return
	}
	fmt.Fprintln(w, what+" download failed")
}
