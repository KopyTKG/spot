package ui

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// readSmall reads a regular file of at most limit bytes. What a config names as
// a stylesheet or an image is read by the daemon: a device or a huge file there
// would take the daemon down.
func readSmall(path string, limit int64) ([]byte, error) {
	// Non-blocking, and checked on the open file: a path swapped for a pipe must
	// not stop the UI thread.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s: not a plain file", path)
	}
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err == nil && int64(len(b)) > limit {
		err = fmt.Errorf("%s: larger than %d bytes", path, limit)
	}
	return b, err
}
