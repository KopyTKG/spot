//go:build !spottest

package main

import "gitlab.com/KopyTKG/spot/internal/approval"

// modStore is where this user's mods and approvals are. Nothing in the
// environment changes it.
func modStore() (approval.Store, error) { return approval.Default() }
