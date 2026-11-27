package ui

import (
	"errors"
	"log"
	"os/exec"
	"strings"

	"gitlab.com/KopyTKG/spot/internal/capture"
)

// copyText puts text on the clipboard. wl-copy gets it as it is, on its input,
// with no shell between; it stays to serve it after we are gone, which a dialog
// shown by its own process could not do itself.
func copyText(text string, env []string, dir string) error {
	c := exec.Command("wl-copy")
	c.Env, c.Dir = env, dir
	c.Stdin = strings.NewReader(text)
	err := c.Run()
	if errors.Is(err, exec.ErrNotFound) {
		return errors.New("wl-copy is not installed (package wl-clipboard)")
	}
	return err
}

// spawn starts cmd and does not wait for it to finish.
func spawn(cmd string, env []string, dir string) {
	c := exec.Command("sh", "-c", cmd)
	c.Env, c.Dir = env, dir
	if err := c.Start(); err != nil {
		log.Printf("spawn: %v", err)
		return
	}
	go c.Wait() // reap it: the daemon outlives its children
}

// runCommand runs cmd to completion and returns what it wrote to stdout and
// stderr and its exit status (0 on success).
func runCommand(cmd string, env []string, dir string) (stdout, stderr string, status int) {
	var so, se capture.Buffer
	c := exec.Command("sh", "-c", cmd)
	c.Env, c.Dir = env, dir
	c.Stdout, c.Stderr = &so, &se
	err := c.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return so.String(), se.String(), 0
	case errors.As(err, &ee):
		return so.String(), se.String(), ee.ExitCode()
	default:
		return so.String(), err.Error() + "\n", 127
	}
}
