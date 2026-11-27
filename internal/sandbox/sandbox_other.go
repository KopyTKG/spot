//go:build !(linux && amd64)

package sandbox

import "errors"

var errUnsupported = errors.New("mods are not supported on this architecture")

// ABI is 0: there is no sandbox here, so no mod runs.
func ABI() int { return 0 }

func Restrict(Policy) error { return errUnsupported }

func Verify() error { return errUnsupported }
