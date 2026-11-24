# spot

**Dialogs and launchers for Wayland, written in TOML and CSS.**

Describe a window in a TOML file (fields, a list, tiles, a search box), style it with GTK
CSS, run `spot config.toml`, and read the answer from stdout or an exit code. The same pieces
build an **application launcher**, a **run bar**, a calculator or a file finder: no code, one
config each.

spot draws one window at a time as a layer-shell overlay, with the keyboard in it, and gets
out of the way: Enter submits, Esc cancels. A small background daemon keeps the window alive
between dialogs so a menu hands over to the next without flicker. It is for the dialog, not for
ambient widgets; [eww](https://github.com/elkowar/eww) and Waybar do that.

```
┌───────────────────────────────────────┐
│ 🔍 = 2+2*10                           │       a launcher: prefix = hands the search
│ ┌───────────────────────────────────┐ │       to a command; the result row is the
│ │ ▦  22                             │ │       answer, Enter copies it
│ │    2+2*10                         │ │
│ └───────────────────────────────────┘ │
└───────────────────────────────────────┘
```

## What it does

- **Dialogs.** `field`, `password`, `text`, `select`, searchable `list` (smart abbreviation
  matching, multi-select, collapsible groups), icon `tiles`, `toggle`, `scale`; validation with
  `pattern` / `required`; submit to stdout, JSON, or a command.
- **Launchers.** `type = "apps"` lists the installed applications with icons and starts them through
  a template you control (`uwsm-app -- {exec}`); **sources** hand the search to your own commands
  behind a prefix (`=` calculator, `.` files, `@` web, `>` run); `hide_empty` and a default source
  make a bare run bar. See the [launcher guide](docs/launcher.md).
- **Scriptable.** stdin feeds lists, `{field}` placeholders feed commands, documented exit codes,
  and a **Back** control so menus nest without extra code.
- **Keybind friendly.** `toggle = true` makes the key close what it opened and stops repeats from
  stacking windows; `SPOT_QUERY` opens a launcher already in a mode.
- **Yours to style and place.** Everything visual is [GTK CSS](docs/css.md); `anchor`, `margin_*`
  and `offset_*` put a dialog anywhere. Dialog config and CSS are re-read on every call.
- **Fast.** The daemon keeps the window (and the launcher's app index) warm.

## A quick look

A form:

```toml
# ssh.toml
title  = "SSH"
width  = 420
style  = "ssh.css"
footer = "Enter to connect · Esc to cancel"

[[items]]
type  = "field"
id    = "user"
label = "User"

[[items]]
type  = "field"
id    = "host"
label = "Host"

[[items]]
type    = "field"
id      = "port"
label   = "Port"
default = "22"

[submit]
exec  = "setsid -f xdg-terminal-exec ssh {user}@{host} -p {port}"
spawn = true
```

```sh
spot ssh.toml        # a path, or just: spot ssh   (looks up ~/.config/spot/ssh.toml)
```

A launcher:

```toml
# ~/.config/spot/launcher.toml
title  = ""
width  = 640
style  = "launcher.css"
toggle = true                      # bound to a key: pressing it again closes it

[[items]]
type        = "apps"
id          = "app"
placeholder = "Search apps…   = calc · . files · @ web"

[apps.launch]
command = "uwsm-app -- {exec}"

[[sources]]                        # type "= 2+2": the command prints the rows
name     = "calc"
prefix   = "="
exec     = "calc.sh {query}"
activate = "wl-copy {value}"
```

```lua
hl.bind("SUPER + SPACE", hl.dsp.exec_cmd("spot launcher"))     -- Hyprland
```

More in [`examples/`](examples/) (indexed in its [README](examples/README.md)), including a help
screen with folding sections, a run bar and the source scripts.

## Using it

```
spot [flags] <config>
```

`<config>` is a path to a TOML file, or a bare name that is looked up as
`${XDG_CONFIG_HOME:-~/.config}/spot/<name>.toml`. A relative `style` path resolves against the
TOML's own directory.

| Flag | |
|------|-|
| `-h`, `--help` · `-v`, `--version` | Usage · the version |
| `--back` | Show a [Back control](docs/toml.md#back); Back exits with status 4 (`SPOT_BACK=<command>` does too, and runs the command) |
| `--chain` · `--last` | Another dialog follows: keep the window up for it · nothing follows: release it at once |
| `--sub` | Open on top of the dialog that is showing; it returns when this one closes |
| `--oneshot` | Do not use the daemon (also `SPOT_NO_DAEMON=1`) |
| `--dismiss` · `--quit` · `--status` | Control the daemon: cancel every dialog and release the window · stop it once idle · report whether it runs |

**Exit codes:** `0` submitted, `1` cancelled (Esc, click outside; nothing printed), `2` usage
error, `3` config or runtime error (message on stderr), `4` Back. With
`[submit] on_error = "exit"` a failing command makes spot exit with that command's status.

**Behaviour.** Enter submits and Esc cancels; in a `text` area Enter inserts a newline and
Ctrl+Enter submits. The layer surface covers the output, so a click outside the dialog closes
it, and while the pointer is outside the dialog fades ([`[opacity]`](docs/toml.md#opacity)).
Concurrent `spot` calls are served by one daemon: a call that arrives while a dialog is up waits
its turn, or with `--sub` opens on top of it. The first call starts the daemon; to have it ready
at login, enable the user service the packages ship (`systemctl --user enable --now spot.service`)
or run `spot daemon` from your compositor's autostart. Whether a dialog offers Back is the
*caller's* decision (`SPOT_BACK` in the environment), so one config can return to a menu when a
menu opened it and have no Back when a key did. Details: [`docs/daemon.md`](docs/daemon.md) and
[`docs/toml.md`](docs/toml.md#back); [`examples/back.sh`](examples/back.sh) is a runnable
walk-through.

## Documentation

| | |
|-|-|
| **[`docs/toml.md`](docs/toml.md)** | Reference: every TOML setting, from the window and its placement to each widget, `apps`, `[[sources]]`, `[submit]` and the `SPOT_*` environment variables |
| **[`docs/launcher.md`](docs/launcher.md)** | Guide: build a launcher, a run bar or any search-and-run picker; recipes, keybinds, styling, troubleshooting |
| **[`docs/css.md`](docs/css.md)** | Styling: the class tree, every `.spot-*` class, states, a starter stylesheet |
| **[`docs/daemon.md`](docs/daemon.md)** | The daemon: on demand, the systemd user service, `daemon.toml`, control flags, troubleshooting |
| [`docs/roadmap.md`](docs/roadmap.md) · [`CHANGELOG.md`](CHANGELOG.md) | Where it is going · what changed in each release |

For **every key in one runnable file**, see
[`examples/config-reference.toml`](examples/config-reference.toml).

## Install

From the AUR: `spot-bin` (the prebuilt release binary) or `spot-git` (built from the repository
head). Both install the README, docs and example configs to `/usr/share/doc/spot/` and a systemd
user unit for the daemon (not enabled; see
[`docs/daemon.md`](docs/daemon.md#systemd-user-service)). Releases are cut manually; see
[`packaging/aur/README.md`](packaging/aur/README.md).

## Build

Requires Go ≥ 1.26 (see `go.mod`), the GTK4 development headers and the gtk4-layer-shell C library.

```sh
# Arch
sudo pacman -S gtk4 gtk4-layer-shell go

go build -o spot .        # or: just build
go test ./...             # or: just test (no display needed)
go test -tags gui ./...   # also the dialog tests; they need a Wayland session (nothing is drawn)
```

`just build` and `just package` bake the git tag into `spot --version`; a plain `go build`
reports `dev`. To set it by hand: `go build -ldflags "-X main.version=1.2.3" -o spot .`

## License

[MIT](LICENSE)
