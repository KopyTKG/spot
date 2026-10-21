# spot

Transactional GUI dialog runtime for Wayland. Write a TOML config describing a
window (title, fields, footer, submit command), point it at a CSS file for
styling, and `spot` renders it as a layer-shell overlay — Enter submits, Esc
cancels.

Fills a gap between `walker --dmenu` (launcher-shaped, compiled-in templates),
`yad` / `zenity` (transactional but preset dialogs, no CSS), and `eww`
(declarative + CSS but daemon-based, built for ambient widgets, not one-shots).

**Status:** early / v0. Widgets: field, password, text, select, list, tiles, toggle, scale.

## Behaviour

- **Enter** submits, **Esc** cancels (exit status 1, nothing printed). In a
  `text` area Enter inserts a newline and **Ctrl+Enter** submits.
- The layer surface covers the whole output with the dialog inside it, so a
  **click outside the dialog closes it**.
- While the pointer is outside the dialog it fades (85% by default, see
  [`[opacity]`](#opacity-optional)).
- Each invocation gets its own app ID, so concurrent `spot` calls don't dedupe.

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

### Exit codes

Scripts wrapping spot can rely on these:

| Code | Meaning |
|------|---------|
| `0` | Submitted |
| `1` | Cancelled (Esc, click outside the dialog); nothing is printed |
| `2` | Usage error (bad flag, missing or extra argument) |
| `3` | Config or runtime error (unreadable file, invalid TOML, bad `pattern`, GTK failure); the message goes to stderr |

With `[submit] on_error = "exit"` a failing `exec` command makes spot exit
with *that command's* status instead.

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
| [`demo/`](examples/demo/) | **0.2.0 tour**: `./examples/demo/demo.sh` launches a form with every input widget, two tile sections, a stdin list and a failing `exec` |
| [`theme.css`](examples/theme.css) | Full theme covering every widget, including validation and error states (used by all but `ssh`) |

## Documentation

Reference docs live in [`docs/`](docs/):

- **[`docs/toml.md`](docs/toml.md)**: every TOML setting. Window keys, `[opacity]`, each widget type (`field`, `password`, `text`, `select`, `list`, `tiles`, `toggle`, `scale`), validation, `[submit]` (`exec`, `stdout`, JSON output, `on_error`, `{_active}`), exit codes and the `SPOT_*` environment overrides.
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
repository head). Both install the README and the example configs to
`/usr/share/doc/spot/`. Releases are cut manually; see
[`packaging/aur/README.md`](packaging/aur/README.md).

## Build

Requires Go ≥ 1.21, GTK4 dev headers, and the gtk4-layer-shell C library.

```sh
# Arch
sudo pacman -S gtk4 gtk4-layer-shell go

go build -o spot .   # or: just build
go test ./...        # or: just test (no display needed)
```

`just build` and `just package` bake the git tag into `spot --version`; a plain
`go build` reports `dev`. To set it by hand:
`go build -ldflags "-X main.version=1.2.3" -o spot .`

## License

[MIT](LICENSE)
