package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
)

// iconSearchPaths adds the icon directories the session may not advertise: the
// flatpak exports (a daemon started before the environment was complete does
// not have them in XDG_DATA_DIRS), the user's own icons and the icons next to
// every applications directory that is scanned.
var iconPathsOnce sync.Once

func ensureIconPaths(appPaths []string, home string) {
	iconPathsOnce.Do(func() {
		display := gdk.DisplayGetDefault()
		if display == nil {
			return
		}
		theme := gtk.IconThemeGetForDisplay(display)
		dirs := []string{
			"/var/lib/flatpak/exports/share/icons",
			filepath.Join(home, ".local/share/flatpak/exports/share/icons"),
			filepath.Join(home, ".local/share/icons"),
			"/usr/share/pixmaps",
		}
		for _, p := range appPaths {
			dirs = append(dirs, filepath.Join(filepath.Dir(strings.TrimRight(p, "/")), "icons"))
		}
		have := map[string]bool{}
		for _, d := range theme.SearchPath() {
			have[d] = true
		}
		for _, d := range dirs {
			if st, err := os.Stat(d); err == nil && st.IsDir() && !have[d] {
				theme.AddSearchPath(d)
				have[d] = true
			}
		}
	})
}

// setAppIcon shows icon (a theme name like "firefox" or an absolute file path)
// in img at size px, falling back to a generic application icon.
func setAppIcon(img *gtk.Image, icon string, size int) {
	img.SetPixelSize(size)
	if filepath.IsAbs(icon) {
		if st, err := os.Stat(icon); err == nil && st.Mode().IsRegular() && st.Size() <= 8<<20 {
			img.SetFromFile(icon)
			return
		}
	}
	display := gdk.DisplayGetDefault()
	if display != nil && icon != "" {
		theme := gtk.IconThemeGetForDisplay(display)
		name := icon
		if !theme.HasIcon(name) {
			name = strings.TrimSuffix(strings.TrimSuffix(icon, ".png"), ".svg") // "Icon=foo.png" names are allowed in old entries
		}
		if theme.HasIcon(name) {
			img.SetFromIconName(name)
			return
		}
	}
	img.SetFromIconName("application-x-executable")
}
