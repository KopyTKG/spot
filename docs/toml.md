# TOML reference

A spot config is one TOML file. Top-level keys describe the window, `[opacity]`
tunes the fade, `[[items]]` are the widgets (top to bottom, in file order) and
`[submit]` says what happens on Enter.

```toml
title = "SSH"
width = 420
style = "theme.css"

[[items]]
type = "field"
id   = "host"

[submit]
stdout = "{host}"
```

For a single commented file with every key and its default, see
[`examples/config-reference.toml`](../examples/config-reference.toml).

## Contents

- [Running it](#running-it)
- [Window](#window)
- [`[opacity]`](#opacity)
- [Items](#items)
  - [Common keys](#common-keys)
  - [`field`](#field) · [`password`](#password) · [`text`](#text) · [`select`](#select)
  - [`list`](#list) · [`tiles`](#tiles) · [`toggle`](#toggle) · [`scale`](#scale)
  - [Validation](#validation)
- [`[submit]`](#submit)
- [Environment overrides](#environment-overrides)

## Running it

```
spot [flags] <config>
```

`<config>` is a path. An argument that is not an existing file, has no `/` and
no `.toml` suffix is looked up as `${XDG_CONFIG_HOME:-~/.config}/spot/<name>.toml`
(`spot power` opens `~/.config/spot/power.toml`).

| Flag | |
|------|-|
| `-h`, `--help` | Usage |
| `-v`, `--version` | Print the version |

| Exit code | Meaning |
|-----------|---------|
| `0` | Submitted |
| `1` | Cancelled (Esc or a click outside the dialog); nothing is printed |
| `2` | Usage error (bad flag, missing or extra argument) |
| `3` | Config or runtime error (unreadable file, invalid TOML, bad `pattern`, GTK failure); message on stderr |

With `on_error = "exit"` a failing `exec` command makes spot exit with *that
command's* status instead.

Enter submits and Esc cancels. In a [`text`](#text) item Enter inserts a
newline; Ctrl+Enter submits. The layer surface covers the whole output, so a
click outside the dialog cancels it.

## Window

| Key | Default | Description |
|-----|---------|-------------|
| `title` | none | Heading shown at the top, also the window title |
| `anchor` | `center` | Where the dialog sits: `center`, `top`, `bottom`, `left`, `right`. Combine with spaces: `top right` |
| `width` | natural | Dialog width in px |
| `height` | natural | Dialog height in px |
| `style` | none | CSS file, relative to this TOML (or absolute). See [css.md](css.md) |
| `footer` | none | Small text at the bottom |
| `keyboard` | `exclusive` | Layer-shell keyboard mode: `exclusive` (always has keys), `on-demand` (focus like a normal window), `none` |

## `[opacity]`

The dialog fades while the pointer is outside it. This is **on by default**
(85% outside, 100% inside); `[opacity]` overrides the values, and
`outside = 100` turns the fade off.

| Key | Default | Description |
|-----|---------|-------------|
| `inside` | `100` | Opacity in % (0–100) while the pointer is over the dialog |
| `outside` | `85` | Opacity in % while the pointer is outside it |
| `transition` | `150` | Fade time in ms (`0` is instant) |

The dialog starts at `inside` until the pointer first moves.

Pressing a key hides the mouse pointer and stops tracking it: the dialog stays
at `inside` until the pointer actually moves again.

```toml
[opacity]
inside     = 100
outside    = 55
transition = 250
```

## Items

### Common keys

| Key | Applies to | Description |
|-----|------------|-------------|
| `type` | all | `field`, `password`, `text`, `select`, `list`, `tiles`, `toggle`, `scale`. An unknown type is skipped with a log line |
| `id` | all | Name used in `{id}` templates and as the JSON key |
| `label` | all | Shown above the widget. For `toggle` it is the checkbox text; for `tiles` it is a section heading |
| `placeholder` | field, password, text, list | Greyed-out hint text |
| `default` | field, text, select, list, tiles, toggle, scale | Initial value. For `tiles` it is a tile `value`; for `toggle` `true` or `false`; for `scale` a number; for a multi `list` the options joined by `separator` |
| `required` | field, password, text, select, list | An empty value blocks submission |
| `pattern` | field, password, text, select, list | A non-empty value must match this [Go regexp](https://pkg.go.dev/regexp/syntax). Unanchored: write `^...$` |
| `error` | any validated item | Message shown under the item when it is invalid |

### `field`

Single-line text entry.

```toml
[[items]]
type        = "field"
id          = "user"
label       = "User"
placeholder = "name"
default     = "root"
```

### `password`

Masked entry. Takes `placeholder`, `required`, `pattern`, `error`. No `default`.

```toml
[[items]]
type     = "password"
id       = "pw"
label    = "Password"
required = true
error    = "Enter a password"
```

### `text`

Multi-line text area. Enter inserts a newline, Ctrl+Enter submits.

| Key | Default | Description |
|-----|---------|-------------|
| `height` | `120` | Minimum content height in px |

```toml
[[items]]
type        = "text"
id          = "note"
label       = "Note"
placeholder = "Anything else?"
height      = 80
```

### `select`

Dropdown.

| Key | Description |
|-----|-------------|
| `options` | Array of strings |
| `default` | One of the options |

```toml
[[items]]
type    = "select"
id      = "shell"
options = ["fish", "bash", "zsh"]
default = "fish"
```

### `list`

Searchable list: type to filter, Up/Down to move, Enter to pick.

| Key | Default | Description |
|-----|---------|-------------|
| `options` | none | Array of strings |
| `stdin` | `false` | Read newline-delimited options from stdin instead (dmenu style) |
| `height` | `320` | Minimum list height in px |
| `match` | `contains` | How typing filters: `contains` (case-insensitive substring) or `smart` (see below) |
| `multi` | `false` | Allow selecting several rows |
| `separator` | newline | Joins the selection of a `multi` list in `{id}` templates |

With `multi = true`: click or Ctrl+click rows; from the search box Up/Down moves
focus into the list, where Space toggles a row and Shift/Ctrl+arrows extend the
selection. Only rows that match the current search count. In
[JSON output](#submit) a multi list is an array of strings.

```toml
[[items]]
type      = "list"
id        = "tags"
multi     = true
separator = ","
options   = ["red", "green", "blue"]
```

```sh
printf '%s\n' alpha beta gamma | spot menu.toml    # stdin = true
```

**Smart matching** (`match = "smart"`) works like abbreviations on a network
device CLI: type a few letters and Enter picks the best hit. Each
whitespace-separated term of the query has to match, case and icons are
ignored, and a single-select list is reordered best match first:

| Typed | Finds | Why |
|-------|-------|-----|
| `rem` | `Remote` | prefix of the text |
| `fire` | `System Firewall` | prefix of a word |
| `sf` | `System Firewall` | word initials |
| `rmt` | `Remote` | letters in order, gaps allowed |
| `sys fw` | `System Firewall` | every term must match |

Prefix matches rank above initials, above substrings, above scattered letters;
ties keep the original order. A `multi` list filters the same way but is not
reordered (reordering would drop the rows you already selected).

### `tiles`

A grid of large icon-and-label buttons. Clicking a tile, or pressing Enter on
it, picks it **and submits the whole dialog**.

| Key | Default | Description |
|-----|---------|-------------|
| `columns` | `3` | Tiles per row |
| `default` | first tile | `value` of the tile selected initially |
| `current` | none | `value` of the tile that is the live setting (say, the power profile in use). It gets a persistent `● active` badge and the `.spot-active` class, whatever is selected. `SPOT_CURRENT_<ID>` sets it from a script |

Each tile is an `[[items.tiles]]` table:

| Key | Description |
|-----|-------------|
| `icon` | Large text above the label (Nerd Font glyph, emoji, a few letters...) |
| `label` | Caption. Omit it to centre the icon alone |
| `value` | What the item evaluates to when this tile is chosen |

```toml
[[items]]
type    = "tiles"
id      = "action"
label   = "Power"
columns = 3

[[items.tiles]]
icon  = "L"
label = "Lock"
value = "lock"
```

**Several sections.** Repeat `[[items]]` with `type = "tiles"`. Every section
always has a value (its selected tile), so a submit needs to say which section
you used: `{_active}` expands to the `id` of the section last interacted with.

```toml
[submit]
stdout = "{_active} {action} {battery}"
```

### `toggle`

A checkbox labelled with `label`.

| Key | Default | Description |
|-----|---------|-------------|
| `on` | `"true"` | Value substituted when checked |
| `off` | `"false"` | Value substituted when unchecked |
| `default` | `false` | Initial state |

```toml
[[items]]
type  = "toggle"
id    = "sudo"
label = "Run as root"
on    = "sudo "
off   = ""
```

### `scale`

Horizontal slider with the value shown.

| Key | Default | Description |
|-----|---------|-------------|
| `min` | `0` | Lower bound (must not exceed `max`) |
| `max` | `100` | Upper bound |
| `step` | `1` | Increment |
| `default` | `min` | Initial value, clamped into range |

```toml
[[items]]
type    = "scale"
id      = "vol"
min     = 0
max     = 100
step    = 5
default = 50
```

### Validation

On submit, every item with `required` or `pattern` is checked. If any fails,
**nothing is run or printed**, the failing items get the `.spot-invalid` class
(and their `error` message appears under them), and the first one takes focus.
The class clears as soon as the item is edited. See [css.md](css.md#validation-and-errors)
for styling it.

```toml
[[items]]
type     = "field"
id       = "port"
required = true
pattern  = "^[0-9]+$"
error    = "Port must be a number"
```

## `[submit]`

| Key | Default | Description |
|-----|---------|-------------|
| `exec` | none | Shell command template, run on submit |
| `spawn` | `false` | `exec` only: fire and forget, then close |
| `stdout` | none | Template printed to stdout on submit |
| `format` | none | `"json"`: print every item's value as one JSON object. Cannot be combined with `stdout` |
| `on_error` | `show` | `exec` without `spawn`: what to do when the command exits non-zero, `show` or `exit` |

### Templates

`{id}` becomes that item's current value; `{_active}` becomes the id of the last
tiles section used. Substitution happens in one pass, so a value that happens to
contain `{other}` is not expanded again. Values are **not** shell-quoted: use
`format = "json"`, or a `pattern` that rules out metacharacters, when a value
ends up in a shell command.

### `exec`

Without `spawn`, `exec` runs to completion before the dialog closes, and its
stdout is passed through. If it exits non-zero:

- `on_error = "show"`: the dialog stays open and the command's stderr appears
  in a `.spot-error` label, so the input can be fixed and retried. Nothing is
  printed.
- `on_error = "exit"`: the dialog closes, stderr is written through and spot
  exits with the command's status.

### JSON output

`format = "json"` prints one object with every item's value, properly escaped:

```sh
$ spot ssh.toml
{"_active":"","host":"box","port":"22","user":"me"}
$ spot ssh.toml | jq -r .host
box
```

Every value is a string, except a `multi` list, which is an array of strings.
`_active` is always present (empty when there are no tiles).

## Environment overrides

Let one shared config be parameterised per call.

| Variable | Effect |
|----------|--------|
| `SPOT_TITLE` | Replaces `title` |
| `SPOT_FOOTER` | Replaces `footer` |
| `SPOT_PLACEHOLDER` | Placeholder of the first field, password, text or list item |
| `SPOT_DEFAULT` | Default of the first item that takes one (field, text, select, list, tiles, toggle, scale) |
| `SPOT_CURRENT_<ID>` | `current` of the `tiles` item with that `id`, same naming as `SPOT_DEFAULT_<ID>` |
| `SPOT_DEFAULT_<ID>` | Default of the item with that `id`, upper-cased with non-alphanumerics as `_` (`battery-mode` becomes `SPOT_DEFAULT_BATTERY_MODE`). Wins over `SPOT_DEFAULT` |

```sh
SPOT_TITLE="Delete 3 files?" SPOT_DEFAULT=no spot confirm.toml
SPOT_DEFAULT_BATTERY=balanced spot power.toml
```
