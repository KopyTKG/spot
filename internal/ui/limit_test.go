package ui

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestReadSmallRefusesWhatIsNotASmallFile(t *testing.T) {
	d := t.TempDir()
	small, big, fifo := filepath.Join(d, "small"), filepath.Join(d, "big"), filepath.Join(d, "fifo")
	os.WriteFile(small, []byte("1234"), 0o600)
	os.WriteFile(big, []byte("12345"), 0o600)
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := readSmall(small, 4); err != nil || string(b) != "1234" {
		t.Errorf("small: %q, %v", b, err)
	}
	for _, p := range []string{big, fifo, "/dev/zero", d} {
		if _, err := readSmall(p, 4); err == nil {
			t.Errorf("%s was accepted", p)
		}
	}
}

// A few bytes on disk can ask for gigabytes once decoded, and finding that out
// must itself be quick: it runs on the UI thread.
func TestImageDimensionsAreBounded(t *testing.T) {
	var small bytes.Buffer
	png.Encode(&small, image.NewRGBA(image.Rect(0, 0, 8, 8)))
	huge := bytes.Clone(small.Bytes())
	binary.BigEndian.PutUint32(huge[16:], 60000) // width and height in IHDR
	binary.BigEndian.PutUint32(huge[20:], 60000)
	binary.BigEndian.PutUint32(huge[29:], crc32.ChecksumIEEE(huge[12:29]))

	svg := func(attrs string) string {
		return `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg" ` + attrs + `/>`
	}
	d := t.TempDir()
	for name, c := range map[string]struct {
		body string
		ok   bool
	}{
		"small.png":   {small.String(), true},
		"huge.png":    {string(huge), false},
		"icon.svg":    {svg(`width="64" height="64"`), true},
		"viewbox.svg": {svg(`viewBox="0 0 512 512"`), true},
		"percent.svg": {svg(`width="100%" height="100%" viewBox="0,0,48,48"`), true},
		"plain.svg":   {svg(``), true},
		"huge.svg":    {svg(`width="60000" height="60000"`), false},
		"units.svg":   {svg(`width="900in" height="900in"`), false},
		"box.svg":     {svg(`viewBox="0 0 60000 60000"`), false},
		"exp.svg":     {svg(`width="6e4" height="6e4"`), false},
		"icon.xpm":    {"/* XPM */", true},
		"huge.ppm":    {"P6\n60000 60000\n255\n", false},
		"text.png":    {"not an image", false},
	} {
		p := filepath.Join(d, name)
		os.WriteFile(p, []byte(c.body), 0o600)
		start := time.Now()
		if got := imageFits(p); got != c.ok {
			t.Errorf("%s: fits = %v, want %v", name, got, c.ok)
		}
		if took := time.Since(start); took > 100*time.Millisecond {
			t.Errorf("%s: checking took %v", name, took)
		}
	}
}

func TestInstalledIconsStillFit(t *testing.T) {
	files, _ := filepath.Glob("/usr/share/pixmaps/*")
	more, _ := filepath.Glob("/usr/share/icons/hicolor/*/apps/*")
	var seen, refused int
	for _, p := range append(files, more...) {
		st, err := os.Stat(p)
		switch filepath.Ext(p) {
		case ".png", ".svg", ".xpm":
		default:
			continue
		}
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		seen++
		if !imageFits(p) {
			refused++
			t.Log("refused:", p, st.Size())
		}
	}
	t.Logf("%d icons, %d refused", seen, refused)
}
