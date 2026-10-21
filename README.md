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

## Config reference

A single file listing every key with its default: [`examples/config-reference.toml`](examples/config-reference.toml).

### Top-level

| Key        | Default       | Description                                              |
|------------|---------------|----------------------------------------------------------|
| `title`    | —             | Window title text (also sets GTK window title)           |
| `anchor`   | `center`      | Where the dialog sits on screen: `center` \| `top` \| `bottom` \| `left` \| `right` (space-separated to combine, e.g. `top right`) |
| `width`    | natural       | Dialog width in px                                       |
| `height`   | natural       | Dialog height in px                                      |
| `style`    | —             | Path to CSS file, relative to this TOML                  |
| `footer`   | —             | Footer label text                                        |
| `keyboard` | `exclusive`   | Layer-shell keyboard mode: `exclusive` \| `on-demand` \| `none` |

### `[opacity]` (optional)

The layer surface covers the whole screen; a click outside the dialog closes it.
The dialog itself fades while the pointer is outside it. This is on by default
(85% outside, 100% inside); `[opacity]` overrides it, and `outside = 100`
turns it off.

| Key          | Default | Description                                              |
|--------------|---------|----------------------------------------------------------|
| `inside`     | `100`   | Dialog opacity in % (0-100) while the pointer is over it |
| `outside`    | `85`    | Dialog opacity in % while the pointer is outside it      |
| `transition` | `150`   | Fade time in ms (`0` = instant)                          |

The dialog starts at `inside` until the pointer first moves.

```toml
[opacity]
inside     = 100
outside    = 55
transition = 200
```

### `[[items]]`

Items are rendered top to bottom in file order.

| Key           | Applies to        | Description                                                                 |
|---------------|-------------------|-----------------------------------------------------------------------------|
| `type`        | all               | `field` \| `password` \| `text` \| `select` \| `list` \| `tiles` \| `toggle` \| `scale` |
| `id`          | all               | Identifier used for `{id}` substitution in `[submit]`                       |
| `label`       | all               | Label rendered above the widget (a section heading for `tiles`)             |
| `placeholder` | field, password, text, list | Greyed-out placeholder text                                       |
| `default`     | field, text, select, list, tiles, toggle, scale | Pre-filled / pre-selected value (for `tiles`, a tile `value`; `toggle` takes `true`/`false`; `scale` a number; a multi `list` the options joined by `separator`) |
| `options`     | select, list      | Array of strings to populate the dropdown / list                            |
| `stdin`       | list              | If `true`, read newline-delimited options from stdin (dmenu-style)          |
| `height`      | list, text        | Minimum content height in px (default 320 for `list`, 120 for `text`)       |
| `multi`       | list              | Allow selecting several rows (see below)                                    |
| `separator`   | list (`multi`)    | Joins the selected options (default newline)                                |
| `on`, `off`   | toggle            | Values substituted when checked / unchecked (default `"true"` / `"false"`)  |
| `min`, `max`, `step` | scale      | Range and increment (default `0`, `100`, `1`)                               |
| `required`    | field, password, text, select, list | Empty value blocks submission                             |
| `pattern`     | field, password, text, select, list | Regexp ([Go syntax](https://pkg.go.dev/regexp/syntax)) a non-empty value must match; unanchored, so write `^…$` |
| `error`       | any validated item | Message shown under the item when it is invalid                            |
| `columns`     | tiles             | Tiles per row (default `3`)                                                 |

#### Widgets

- **`text`**: multi-line `GtkTextView` with `height` and `placeholder`. Enter
  inserts a newline, Ctrl+Enter submits.
- **`toggle`**: a check button labelled with `label`. Substitutes `on` when
  checked and `off` otherwise.
- **`scale`**: horizontal slider from `min` to `max` in `step` increments,
  starting at `default` (clamped into range).
- **`list` with `multi = true`**: several rows can be selected. Click or
  Ctrl+click; from the search box, Up/Down moves focus into the list, where
  Space toggles the row and Shift/Ctrl+arrows extend the selection. The value
  is the selected options joined by `separator`. Only rows matching the current
  search count.

```toml
[[items]]
type      = "list"
id        = "pkgs"
multi     = true
separator = ","
options   = ["git", "go", "just"]

[[items]]
type    = "scale"
id      = "vol"
min     = 0
max     = 100
step    = 5
default = 50

[[items]]
type    = "toggle"
id      = "sudo"
label   = "Run as root"
on      = "sudo "
off     = ""
```

#### Validation

On submit, every item with `required` or `pattern` is checked. If any fails,
nothing is run or printed, the invalid items get the `.spot-invalid` class (plus
their `error` message, if any) and the first one takes focus. The class is
cleared as soon as the text is edited.

```toml
[[items]]
type     = "field"
id       = "port"
required = true
pattern  = "^[0-9]+$"
error    = "Port must be a number"
```

#### `[[items.tiles]]`

Tile picker: a grid of big icon + label buttons. Click or Enter picks one and
submits. Repeat `[[items]]` with `type = "tiles"` for several sections (see
`{_active}` below).

| Key     | Description                                                  |
|---------|--------------------------------------------------------------|
| `icon`  | Large text above the label (Nerd Font glyph, emoji, …)       |
| `label` | Tile caption (omit to centre the icon alone)                 |
| `value` | Returned as the item's value when this tile is chosen        |

### `[submit]`

| Key      | Default | Description                                                                |
|----------|---------|----------------------------------------------------------------------------|
| `exec`   | —       | Shell command template, run on submit                                      |
| `spawn`  | `false` | (`exec` only) fire-and-forget, then close the window                       |
| `stdout` | —       | Template printed to stdout on submit, for scripts to capture               |
| `format` | —       | `"json"`: print every item's value as one JSON object (see below). Cannot be combined with `stdout` |
| `on_error` | `show` | (`exec` without `spawn`) what to do when the command exits non-zero: `show` or `exit` |

Templates substitute `{id}` with that item's current value. `{_active}` expands
to the `id` of the tiles section last interacted with, so a menu with several
tile sections can tell which one was used:

```toml
[submit]
stdout = "{_active} {action} {battery}"
```

Without `spawn`, `exec` runs to completion before the dialog closes; its stdout
is passed through. If it exits non-zero:

- `on_error = "show"` (default): the dialog stays open and the command's stderr
  appears in a `.spot-error` label at the bottom, so the user can fix the input
  and try again. Nothing is printed to stdout.
- `on_error = "exit"`: the dialog closes, stderr is written through, and spot
  exits with the command's status.

`format = "json"` makes scripts independent of template parsing; values are
escaped properly:

```sh
$ spot ssh.toml
{"_active":"","host":"box","port":"22","user":"me"}
$ spot ssh.toml | jq -r .host
```

Every value is a string, except a `multi` list, which is an array of strings.
`_active` is always present.

## Environment overrides

Lets one shared config be parameterised per invocation:

| Variable           | Effect                                                              |
|--------------------|---------------------------------------------------------------------|
| `SPOT_TITLE`       | Replaces `title`                                                    |
| `SPOT_FOOTER`      | Replaces `footer`                                                   |
| `SPOT_PLACEHOLDER` | Placeholder of the first field / password / text / list item        |
| `SPOT_DEFAULT`     | Default of the first field / text / select / list / tiles / toggle / scale item |
| `SPOT_DEFAULT_<ID>` | Default of the item with that `id` (upper-cased, non-alphanumerics as `_`), e.g. `SPOT_DEFAULT_BATTERY=balanced`; wins over `SPOT_DEFAULT` |

```sh
SPOT_DEFAULT=balanced spot power-profile.toml
```

## CSS hooks

Layout is decided by widget choice in the TOML (GTK4 CSS has no flex/grid —
only visual styling). The runtime adds these classes; style them however you
want:

| Class           | Widget                                  |
|-----------------|-----------------------------------------|
| `.spot-window`  | Full-screen layer surface (keep it `background: transparent`) |
| `.spot-root`    | The dialog: vertical box wrapping everything (opacity fade applies here) |
| `.spot-title`   | Title label                             |
| `.spot-item`    | Per-item container (label + widget)     |
| `.spot-label`   | Item label                              |
| `.spot-field`   | Text input (`GtkEntry`)                 |
| `.spot-password`| Password input (`GtkPasswordEntry`)     |
| `.spot-select`  | Dropdown (`GtkDropDown`)                |
| `.spot-search`  | `list` search entry                     |
| `.spot-list`, `.spot-list-scroll`, `.spot-list-row` | `list` view, its scroller, and rows |
| `.spot-tiles`   | `tiles` grid (`GtkFlowBox`)             |
| `.spot-tile`, `.spot-tile-body`, `.spot-tile-icon`, `.spot-tile-label` | A tile, its content box, icon and caption |
| `.spot-text`, `.spot-text-scroll`, `.spot-text-placeholder` | `text` view, its scroller and placeholder |
| `.spot-toggle`  | `toggle` check button                   |
| `.spot-scale`   | `scale` slider                          |
| `.spot-invalid` | Added to an item's widget that failed `required` / `pattern` |
| `.spot-item-error` | An item's `error` message             |
| `.spot-error`   | Failed `exec` message (`on_error = "show"`) |
| `.spot-footer`  | Footer label                            |

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
