# spot

Transactional GUI dialog runtime for Wayland. Write a TOML config describing a
window (title, fields, footer, submit command), point it at a CSS file for
styling, and `spot` renders it as a layer-shell overlay — Enter submits, Esc
cancels.

Fills a gap between `walker --dmenu` (launcher-shaped, compiled-in templates),
`yad` / `zenity` (transactional but preset dialogs, no CSS), and `eww`
(declarative + CSS, but built for ambient widgets rather than one-shot
dialogs). spot is for the dialog: ask, answer, done. A small background daemon
(see [Daemon](#daemon)) keeps one window alive so chained dialogs hand over
without flicker; it holds no state of its own and draws nothing ambient.

**Status:** early / v0. Widgets: field, password, text, select, list, tiles, toggle, scale.

## Behaviour

- **Enter** submits, **Esc** cancels (exit status 1, nothing printed). In a
  `text` area Enter inserts a newline and **Ctrl+Enter** submits.
- The layer surface covers the whole output with the dialog inside it, so a
  **click outside the dialog closes it**.
- While the pointer is outside the dialog it fades (85% by default, see
  [`[opacity]`](docs/toml.md#opacity)).
- Concurrent `spot` calls are served by one daemon: a call that arrives while a
  dialog is up **waits its turn**, or with `--sub` opens **on top** of it (see
  [Daemon](#daemon)).
- A dialog can offer **Back** (button, **Backspace** on an empty search or on
  tiles, **Alt+Left**), but only when its caller asks for it (see
  [Back](#back)).

## Example

```toml
# ssh.toml
title    = "SSH"
anchor   = "center"
width    = 420
style    = "ssh.css"
footer   = "Enter to connect · Esc to cancel"

[[items]]
type = "field"
id   = "user"
label = "User"
placeholder = "user"

[[items]]
type = "field"
id   = "host"
label = "Host"
placeholder = "host or IP"

[[items]]
type = "field"
id   = "port"
label = "Port"
default = "22"

[submit]
exec  = "setsid -f uwsm-app -- alacritty --class ssh-session -e ssh {user}@{host} -p {port}"
spawn = true
```

```sh
spot ssh.toml
spot ssh          # no such file here: looks up ~/.config/spot/ssh.toml
```

## Usage

```
spot [flags] <config>
```

`<config>` is a path to a TOML file. An argument that is not an existing file
and has no `/` and no `.toml` suffix is looked up as
`${XDG_CONFIG_HOME:-~/.config}/spot/<name>.toml`. A relative `style` path always
resolves against the directory of the TOML that was found.

| Flag | |
|------|-|
| `-h`, `--help` | Usage and a link to this reference |
| `-v`, `--version` | Print the version |
| `--back` | Show a [Back control](docs/toml.md#back) (`SPOT_BACK=<command>` does too, and runs the command); Back exits with status 4 |
| `--chain` | Another dialog follows this one: keep the window up for it (see [Daemon](#daemon)) |
| `--last` | Nothing follows: release the window immediately |
| `--sub` | Open on top of the dialog that is showing; it returns when this one closes |
| `--oneshot` | Do not use the daemon (also `SPOT_NO_DAEMON=1`) |
| `--dismiss` | Cancel every dialog the daemon is showing or holding, and release the window |
| `--quit` | Stop the daemon once it is idle |
| `--status` | Report whether the daemon is running |

### Daemon

Dialogs are shown by a background process, `spot daemon`, that owns one
layer-shell window. `spot <config>` is a thin client: it hands the config path,
your environment and any stdin to the daemon, waits, then prints the answer and
exits with the same status as before. Scripts do not change.

The point is that **moving from one dialog to the next reuses the window.** A
menu that opens a sub-menu used to destroy one surface and create another, and
the fade started over each time. Now the content is swapped in place.

- **Starting.** The first `spot` call starts the daemon (a moment slower, once).
  To have it ready at login, enable the user service the packages ship:
  `systemctl --user enable --now spot.service`, or run `spot daemon` from your
  compositor's autostart.
- **Chaining.** When a dialog closes, the window stays mapped for ~120 ms with
  the dialog hidden. If the script asks for another dialog in that time it takes
  over the same surface; otherwise the surface is released. `--chain` keeps an
  *answered* dialog visible (up to 1.5 s) so a menu hands over without any gap;
  `--last` releases at once. Both times are set in `daemon.toml`.
- **Several at a time.** A request that arrives while a dialog is up waits its
  turn. With `--sub` it opens *on top* instead, and the dialog below comes back,
  state intact, when it closes.
- **Live config.** Dialog TOML and CSS are read on every request. `SPOT_*`
  variables and `submit.exec` commands use the *caller's* environment and
  working directory, not the daemon's.
- **Fallback.** If no daemon can be reached or started (no Wayland session,
  `--oneshot`), spot shows the dialog in a window of its own.

Optional settings (socket, log file, timing) live in
`~/.config/spot/daemon.toml`; see [`examples/daemon.toml`](examples/daemon.toml).
The service, `daemon.toml`, the control flags and troubleshooting are covered in
**[`docs/daemon.md`](docs/daemon.md)**.

### Exit codes

Scripts wrapping spot can rely on these:

| Code | Meaning |
|------|---------|
| `0` | Submitted |
| `1` | Cancelled (Esc, click outside the dialog); nothing is printed |
| `2` | Usage error (bad flag, missing or extra argument) |
| `3` | Config or runtime error (unreadable file, invalid TOML, bad `pattern`, GTK failure); the message goes to stderr |
| `4` | Back (the [Back control](docs/toml.md#back), Backspace on an empty search, or Alt+Left); nothing is printed |

With `[submit] on_error = "exit"` a failing `exec` command makes spot exit
with *that command's* status instead.

### Back

Whether a dialog should offer Back is the caller's call, not the config's, so
the same config can go back to a menu when a menu opened it and have no Back
when a keybind did. The caller says so in the environment, which child scripts
inherit:

```sh
# menu.sh: "Display" opens a script whose dialogs should return here.
SPOT_BACK="menu.sh settings" ./display.sh    # display.sh needs no changes
./display.sh                                 # from a keybind: no Back
```

`SPOT_BACK=<command>` shows the Back control and runs `<command>` when it is
used. `--back` shows it without a command, for scripts that handle navigation
themselves: Back then ends the dialog with **exit status 4** and prints
nothing. Esc is unchanged and still closes the dialog (status 1). Details and
the `[back]` table are in [`docs/toml.md`](docs/toml.md#back); a runnable
walk-through is [`examples/back.sh`](examples/back.sh).

More in [`examples/`](examples/):

| File | Shows |
|------|-------|
| [`ssh.toml`](examples/ssh.toml) + [`ssh.css`](examples/ssh.css) | Form of fields, `exec` + `spawn` submit |
| [`config-reference.toml`](examples/config-reference.toml) | **Every setting** in one commented file, with defaults |
| [`power.toml`](examples/power.toml) | Two-section tile menu using `{_active}` |
| [`confirm.toml`](examples/confirm.toml) | Yes/No tiles, `SPOT_TITLE` / `SPOT_DEFAULT` |
| [`password.toml`](examples/password.toml) | Password prompt printed to stdout |
| [`menu.toml`](examples/menu.toml) | dmenu-style searchable list from stdin |
| [`form.toml`](examples/form.toml) | Field + dropdown with an `exec` submit |
| [`widgets.toml`](examples/widgets.toml) | text, toggle, scale, multi-select list, validation and JSON output |
| [`daemon.toml`](examples/daemon.toml) | Daemon settings (socket, log, timing), every key with its default; not a dialog |
| [`back.sh`](examples/back.sh) | A menu → submenu → dialog flow with Back, and the same dialog opened directly with none: `--back`, `SPOT_BACK`, exit status 4 |
| [`demo/`](examples/demo/) | **0.2.0 tour**: `./examples/demo/demo.sh` launches a form with every input widget, two tile sections, a stdin list and a failing `exec` |
| [`theme.css`](examples/theme.css) | Full theme covering every widget, including validation and error states (used by all but `ssh`) |

## Documentation

Reference docs live in [`docs/`](docs/):

- **[`docs/toml.md`](docs/toml.md)**: every TOML setting. Window keys, `[opacity]`, `[back]`, each widget type (`field`, `password`, `text`, `select`, `list`, `tiles`, `toggle`, `scale`), validation, `[submit]` (`exec`, `stdout`, JSON output, `on_error`, `{_active}`), exit codes and the `SPOT_*` environment overrides.
- **[`docs/daemon.md`](docs/daemon.md)**: the daemon. Running it on demand, as a systemd user service or from compositor autostart, `daemon.toml`, `--status` / `--dismiss` / `--quit`, how dialogs share the window, and troubleshooting.
- **[`docs/css.md`](docs/css.md)**: styling. The class tree, every `.spot-*` class, states and inner GTK nodes, validation styles, how the fade interacts with CSS, and a starter stylesheet.

For every key in one runnable file, see [`examples/config-reference.toml`](examples/config-reference.toml).

### At a glance

```toml
title  = "Example"
width  = 420
style  = "theme.css"            # GTK CSS, see docs/css.md

[opacity]                       # optional; fades the dialog when the pointer leaves it
outside = 85

[[items]]                       # field | password | text | select | list | tiles | toggle | scale
type     = "field"
id       = "port"
label    = "Port"
required = true
pattern  = "^[0-9]+$"
error    = "Port must be a number"

[submit]
format = "json"                 # or stdout = "{port}", or exec = "..."
```

## Install

From the AUR: `spot-bin` (prebuilt release binary) or `spot-git` (built from the
repository head). Both install the README, docs and example configs to
`/usr/share/doc/spot/`, and a systemd user unit for the daemon (not enabled;
see [`docs/daemon.md`](docs/daemon.md#systemd-user-service)). Releases are cut manually; see
[`packaging/aur/README.md`](packaging/aur/README.md).

## Build

Requires Go ≥ 1.26 (see `go.mod`), GTK4 dev headers, and the gtk4-layer-shell C library.

```sh
# Arch
sudo pacman -S gtk4 gtk4-layer-shell go

go build -o spot .   # or: just build
go test ./...        # or: just test (no display needed)
go test -tags gui ./...   # also the dialog tests; they need a Wayland session (nothing is drawn)
```

`just build` and `just package` bake the git tag into `spot --version`; a plain
`go build` reports `dev`. To set it by hand:
`go build -ldflags "-X main.version=1.2.3" -o spot .`

## License

[MIT](LICENSE)
