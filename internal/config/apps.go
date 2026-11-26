package config

// AppsConfig is the top-level [apps] table.
type AppsConfig struct {
	Paths       []string      `toml:"paths"`              // directories to scan (default: XDG data dirs + flatpak exports)
	Exclude     []string      `toml:"exclude"`            // globs on the desktop id or the name
	Desktop     []string      `toml:"desktop"`            // names OnlyShowIn / NotShowIn are checked against (default $XDG_CURRENT_DESKTOP)
	ShowComment *bool         `toml:"show_comment"`       // "Name  —  comment" (default true)
	Icons       *bool         `toml:"icons"`              // show each app's icon (default true)
	IconSize    int           `toml:"icon_size"`          // icon size in px (default 32)
	IconSizeSel int           `toml:"icon_size_selected"` // icon size of the selected row (default: icon_size)
	MaxResults  int           `toml:"max_results"`        // 0 = all
	Frecency    *bool         `toml:"frecency"`           // rank by how often / recently launched (default true)
	Launch      AppsLaunch    `toml:"launch"`
	Override    []AppOverride `toml:"override"`
}

// AppsLaunch says how an application is started.
type AppsLaunch struct {
	Command  string            `toml:"command"`  // template, {exec} = the resolved Exec line (default "{exec}")
	Terminal string            `toml:"terminal"` // wraps Terminal=true apps, {exec} as above (default "xdg-terminal-exec {exec}")
	Cwd      string            `toml:"cwd"`      // working directory (default: the entry's Path=, else the caller's)
	Env      map[string]string `toml:"env"`      // extra environment
}

// AppOverride replaces the launch command of one application, by desktop id.
type AppOverride struct {
	ID      string `toml:"id"`
	Command string `toml:"command"` // the whole command; {exec} still works
}

func (a *AppsConfig) CommentShown() bool { return a == nil || a.ShowComment == nil || *a.ShowComment }

func (a *AppsConfig) IconsShown() bool { return a == nil || a.Icons == nil || *a.Icons }

func (a *AppsConfig) IconPx() int {
	if a == nil || a.IconSize <= 0 {
		return 32
	}
	return a.IconSize
}

func (a *AppsConfig) SelectedIconPx() int {
	if a == nil || a.IconSizeSel <= 0 {
		return a.IconPx()
	}
	return a.IconSizeSel
}

func (a *AppsConfig) UsesFrecency() bool { return a == nil || a.Frecency == nil || *a.Frecency }
