package config

import (
	"fmt"
	"io"
	"os"
)

const maxConfig = 4 << 20

// ReadFile reads a config file, up to a size no real config comes near. A pipe
// is allowed (`spot <(generate)`), so the limit is on what is read.
func ReadFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxConfig+1))
	if err == nil && len(b) > maxConfig {
		err = fmt.Errorf("%s: larger than %d bytes", path, maxConfig)
	}
	return b, err
}

// readRegular is ReadFile for a file that must be an ordinary one, such as an
// included one: a pipe there would hold the daemon.
func readRegular(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return ReadFile(path)
}
