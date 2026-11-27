package ui

import (
	"fmt"
	"io"
	"os"
)

// readSmall reads a regular file of at most limit bytes. What a config names as
// a stylesheet or an image is read by the daemon: a device or a huge file there
// would take the daemon down.
func readSmall(path string, limit int64) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() || st.Size() > limit {
		return nil, fmt.Errorf("%s: not a file of at most %d bytes", path, limit)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, limit))
}
