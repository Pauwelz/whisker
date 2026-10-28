package contract

import (
	"fmt"
	"io"
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
