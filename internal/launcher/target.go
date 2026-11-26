package launcher

import (
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

// targetVar names the file or URL the picked application is opened with:
//
//	SPOT_TARGET=~/notes.md spot launcher
const targetVar = "SPOT_TARGET"

// target is what an application is launched with; the zero value is nothing.
type target struct {
	path string // for %f and %F: an absolute path, or the URL when it names no local file
	uri  string // for %u and %U
}

func (t target) empty() bool { return t.uri == "" }

// isURL tells a URL from a file name. Only "scheme://" and a few schemes written
// without slashes count: "notes: final.txt" is a file.
var isURL = regexp.MustCompile(`^(?:[A-Za-z][A-Za-z0-9+.-]*://|(?:mailto|magnet|tel|sms|geo|xmpp|data):)`).MatchString

// newTarget reads a path or URL. A relative path is relative to dir, the
// caller's: the application may be started somewhere else. Being absolute, a
// path can never be mistaken for an option.
func newTarget(raw, dir string) target {
	switch {
	case raw == "":
		return target{}
	case isURL(raw):
		t := target{path: raw, uri: raw}
		if u, err := url.Parse(raw); err == nil && strings.EqualFold(u.Scheme, "file") && u.Path != "" &&
			(u.Host == "" || u.Host == "localhost") {
			t.path = u.Path
		}
		return t
	}
	p := raw
	if !filepath.IsAbs(p) {
		p = filepath.Join(dir, p)
	}
	return target{path: p, uri: (&url.URL{Scheme: "file", Path: p}).String()}
}
