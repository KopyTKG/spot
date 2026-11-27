//go:build spottest

package main

import (
	"os"

	"gitlab.com/KopyTKG/spot/internal/approval"
)

// modStore, in a binary built for testing only, is where SPOT_TEST_HOME says.
// No released spot is built with this.
func modStore() (approval.Store, error) {
	h := os.Getenv("SPOT_TEST_HOME")
	return approval.Store{Mods: h + "/mods", Records: h + "/approvals", Home: h}, nil
}
