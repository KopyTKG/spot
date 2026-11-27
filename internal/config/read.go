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
