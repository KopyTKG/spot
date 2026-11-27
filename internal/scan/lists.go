package scan

import (
	"net"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gitlab.com/KopyTKG/spot/internal/mod"
)

// What is known to be used for harm and has honest uses too. The list is spot's:
// a mod cannot change it or switch a finding off.

type kind struct{ why, programs string }

var programKinds = []kind{
	{"can send data to other machines; honest use: fetching information",
		"curl wget nc ncat netcat socat ssh scp sftp rsync ftp telnet aria2c httpie http xh"},
	{"can run any other command, within the sandbox; honest use: text processing and glue",
		"sh bash zsh fish dash ksh csh tcsh python python3 node deno bun perl ruby lua luajit php awk gawk mawk nawk sed xargs env find tar git make cmake meson ninja nohup setsid timeout parallel systemd-run " +
			"busybox toybox coreutils flock nice ionice chrt taskset stdbuf unshare strace ltrace gdb lldb script watch expect tclsh wish java jshell kotlin dotnet mono julia R Rscript guile sbcl ghci swift go cargo rustc " +
			"vi vim nvim emacs ed less more man ld.so"},
	{"opens a terminal, an application or a menu, and what that runs; honest use: showing you something",
		"foot kitty alacritty wezterm ghostty xterm konsole gnome-terminal xdg-terminal-exec xdg-open gio rofi wofi dmenu fuzzel spot"},
	{"installs or runs software from elsewhere",
		"npm npx yarn pnpm pip pipx uv gem composer mise asdf pacman yay paru makepkg nix apt dnf"},
	{"can carry data to other machines, also as a name to look up",
		"getent host nslookup resolvectl delv drill ping tracepath traceroute nmap whois gh glab mail sendmail msmtp mutt aws gcloud az kubectl rclone kdeconnect-cli yt-dlp"},
	{"asks for more rights; it cannot work in the sandbox, so why is it there?",
		"sudo pkexec doas su run0"},
	{"controls your desktop or services, and through them can start anything",
		"hyprctl swaymsg niri systemctl busctl dbus-send gdbus loginctl docker podman flatpak"},
	{"changes what runs later or what may be executed; honest use: managing your own files",
		"crontab at chmod chown ln install"},
	{"encodes or moves raw data, often to hide it; honest use: images and disks",
		"base64 base32 xxd od dd openssl gpg"},
	{"reads your clipboard or screen, or types for you",
		"wl-paste wl-copy grim slurp wf-recorder wtype ydotool cliphist"},
	{"reads stored passwords", "secret-tool pass gopass bw op keepassxc-cli"},
}

var programWhy = func() map[string]string {
	m := map[string]string{}
	for _, k := range programKinds {
		for _, p := range strings.Fields(k.programs) {
			m[p] = k.why
		}
	}
	return m
}()

var version = regexp.MustCompile(`[0-9.]+$`)

func (s *scanner) program(line int, program, verb string) {
	if strings.ContainsFunc(program, func(r rune) bool { return r > unicode.MaxASCII || r <= ' ' }) {
		s.flag(line, "it may look like another program's name", "%s a name with unusual characters, %s", verb, Quote(program))
	}
	if strings.ContainsRune(program, '/') {
		s.flag(line, "a program that can be changed after you approve the mod, unless it belongs to the system", "%s the file %s", verb, Quote(program))
	}
	base := filepath.Base(program)
	if strings.HasPrefix(base, "ld-linux") || strings.HasPrefix(base, "ld-musl") {
		base = "ld.so" // the loader runs whatever program it is given
	}
	why, ok := programWhy[base]
	if !ok { // python3.14, node20
		why, ok = programWhy[version.ReplaceAllString(base, "")]
	}
	if ok {
		s.flag(line, why, "%s %s", verb, Quote(base))
	}
}

type place struct {
	re  *regexp.Regexp
	why string
	// writeOnly: reading it is harmless, so a read permission is not flagged.
	writeOnly bool
}

var places = []place{
	{regexp.MustCompile(`\.ssh(/id_|/?$)|id_(rsa|ed25519|ecdsa)|\.pem$`),
		"private keys; tools that list SSH hosts need ~/.ssh/config, not keys", false},
	{regexp.MustCompile(`keyrings?\b|kwallet|\.gnupg|password-store|\.netrc|\.git-credentials|\.aws\b|\.kube\b|\.docker/config|(^|/)\.env$|/etc/shadow`),
		"passwords and tokens", false},
	{regexp.MustCompile(`\.mozilla|librewolf|chromium|google-chrome|BraveSoftware|vivaldi|zen-browser|Cookies\b|Login Data`),
		"a browser's profile: cookies and saved logins; honest use: bookmarks", false},
	{regexp.MustCompile(`\.(bashrc|bash_profile|zshrc|profile|xinitrc|xprofile)\b|config/fish|autostart|systemd/user|/etc/(profile|cron)|config/hypr|config/sway|\.local/bin`),
		"anything written here runs later, outside the sandbox; honest use: reading your settings", true},
	{regexp.MustCompile(`mimeapps\.list|share/applications|dbus-1/services|(^|/)\.gitconfig|config/git`),
		"anything written here decides what is started later; honest use: reading your settings", true},
	{regexp.MustCompile(`_history\b|fish_history|\.histfile`),
		"what you have typed, often with secrets in it", false},
}

// path flags a sensitive place. readOnly: it is only asked to be read.
func (s *scanner) path(line int, v string, readOnly bool) {
	for _, p := range places {
		if p.re.MatchString(v) {
			if !readOnly || !p.writeOnly {
				s.flag(line, p.why, "names %s", Quote(v))
			}
			return
		}
	}
}

var (
	address = regexp.MustCompile(`(?i)\b(https?|ftp|wss?|tcp|ssh)://[^\s"']+`)
	ipv4    = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)
	ipv6    = regexp.MustCompile(`[0-9a-fA-F]{0,4}(:[0-9a-fA-F]{0,4}){2,7}`)
	encoded = regexp.MustCompile(`[A-Za-z0-9+/=_-]{80,}`)
	escapes = regexp.MustCompile(`(\\[0-9]{1,3}){6,}|(\\x[0-9a-fA-F]{2}){6,}`)
)

// text looks at one literal string.
func (s *scanner) text(line int, v string) {
	s.path(line, v, false)
	if m := address.FindString(v); m != "" {
		s.flag(line, "where data can be sent or fetched from", "contains the address %s", Quote(m))
	}
	for _, m := range append(ipv4.FindAllString(v, -1), ipv6.FindAllString(v, -1)...) {
		if ip := net.ParseIP(m); ip != nil && !ip.IsLoopback() && !ip.IsUnspecified() {
			s.flag(line, "a fixed machine to talk to", "contains the IP address %s", Quote(m))
		}
	}
	if m := encoded.FindString(v); m != "" {
		s.flag(line, "can hide what the code does; honest use: an embedded icon", "a long encoded string, %s", Quote(m))
	}
}

// rawText looks for what parsing hides: text written as numbers.
func (s *scanner) rawText(src []byte) {
	for i, l := range strings.Split(string(src), "\n") {
		if escapes.MatchString(l) {
			s.flag(i+1, "text that cannot be read as written; honest use: binary markers", "text written as escape codes, on line %d", i+1)
		}
	}
}

// ProgramRisk says why a program deserves a second look, "" if it is not known.
func ProgramRisk(name string) string {
	s := &scanner{seen: map[string]int{}, perms: &mod.Permissions{}}
	s.program(0, name, "runs")
	var why []string
	for _, n := range s.r.Flagged {
		why = append(why, n.Why)
	}
	return strings.Join(why, "; ")
}

// PlaceRisk says why a file or folder permission deserves a second look: what
// it holds, or what writing there would do. "" if neither is known.
func PlaceRisk(path string, write bool) string {
	if wideFolders.MatchString(filepath.Clean(path)) {
		return "everything under it, with keys, tokens and other programs' files"
	}
	for _, p := range places {
		if p.re.MatchString(path) && (write || !p.writeOnly) {
			return p.why
		}
	}
	return ""
}
