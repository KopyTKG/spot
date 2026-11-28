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
- [`[back]`](#back)
- [Items](#items)
  - [Common keys](#common-keys)
  - [`field`](#field) · [`password`](#password) · [`text`](#text) · [`select`](#select)
  - [`list`](#list) · [`apps`](#apps) · [`tiles`](#tiles) · [`toggle`](#toggle) · [`scale`](#scale)
  - [Collapsible lists](#collapsible-lists)
  - [Validation](#validation)
- [`[apps]`](#apps-1) · [`[[sources]]`](#sources)
- [`[submit]`](#submit)
- [Menus](#menus)
- [Environment overrides](#environment-overrides)

## Mods

`from`, `call`, `run` and `parse` connect items to [mods](modding.md#in-a-dialog), which are in
development. A value that a mod or a parser chose is never pasted into `submit.exec`: read it from
`$SPOT_VALUE_<ID>`. A value starting with `-` stops the submit unless the item sets
`allow_leading_dash = true`.

`[submit]` can do two things without a shell:

```toml
[submit]
copy = "{row}"                 # put it on the clipboard (needs wl-clipboard)

[submit]
call = "network.connect"       # or: make a mod's request, instead of exec
args = ["{row}", "{band}"]     # its arguments
```

A `password` item cannot be used in `copy` or `args`.

## Running it

A first argument of `daemon`, `get`, `watch`, `call`, `parse` or `mods` is a command, not a config. A
config with one of those names is opened by its path: `spot ./watch.toml`.

**A config is a program.** It can run commands when a dialog opens (`state`), as you
type (`[[sources]]`), and when you submit. Open only configs you would run as a
script, and read one from someone else first.

A dialog with a `password` item is always shown by the calling process, never by the
daemon, so the password does not pass through it. A dialog takes at most 8 MB on
stdin.

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
| `--back` | Show a [Back control](#back); Back exits with status 4 |
| `--chain` | Another dialog follows this one: keep the window up for it |
| `--last` | Nothing follows: release the window immediately |
| `--sub` | Open on top of the dialog that is showing; it returns when this one closes |
| `--oneshot` | Do not use the daemon (also `SPOT_NO_DAEMON=1`) |
| `--dismiss`, `--quit`, `--status` | Control the daemon: cancel everything and release the window / stop it once idle / report whether it runs |

Dialogs are shown by a background daemon, started on first use, so chained
dialogs reuse one window. `--chain`, `--last`, `--sub` and the daemon controls
are described in [`daemon.md`](daemon.md), with the systemd service and
`daemon.toml`.

| Exit code | Meaning |
|-----------|---------|
| `0` | Submitted |
| `1` | Cancelled (Esc or a click outside the dialog); nothing is printed |
| `2` | Usage error (bad flag, missing or extra argument) |
| `3` | Config or runtime error (unreadable file, invalid TOML, bad `pattern`, GTK failure); message on stderr |
| `4` | [Back](#back): the Back control, Backspace on an empty search or on tiles, or Alt+Left; nothing is printed |

With `on_error = "exit"` a failing `exec` command makes spot exit with *that
command's* status instead.

Enter submits and Esc cancels. In a [`text`](#text) item Enter inserts a
newline; Ctrl+Enter submits. When the caller offers [Back](#back), Backspace
(on tiles, a list, or an empty search box) and Alt+Left go back. The layer surface covers the whole output, so a
click outside the dialog cancels it.

**Moving around.** Up and Down go from item to item; Left and Right work within one: a slider, the
segments of a `segmented`, a row of tiles. Enter or Space flips or presses what the keyboard is on (in a list with switch rows, Space does so
while the search is empty).
In a `list` and a `text` item they move through the rows and lines, and go on to the next item past the
first or last one. With several lists, the keys are for the one the keyboard is in. The focused control
has `:focus-visible` to style.

## Window

| Key | Default | Description |
|-----|---------|-------------|
| `title` | none | Heading shown at the top, also the window title |
| `anchor` | `center` | Where the dialog sits: `center`, `top`, `bottom`, `left`, `right`. Combine with spaces: `top right` |
| `margin_top`, `margin_bottom`, `margin_left`, `margin_right` | `0` | Pixels kept free between the dialog and that screen edge. They only matter on an edge the anchor sits on (`anchor = "top"` + `margin_top = 120` puts it 120 px from the top) |
| `offset_x`, `offset_y` | `0` | Shift the dialog from where the anchor puts it, in pixels; **+ is right / down, − left / up**, whatever the anchor. Works on a centred axis too (`offset_y = -80` lifts a centred dialog 80 px above the middle). It is added to the margins and never pushes the dialog past an edge |
| `width` | natural | Dialog width in px |
| `height` | natural | Dialog height in px |
| `style` | none | CSS file, relative to this TOML (or absolute). See [css.md](css.md) |
| `footer` | none | Small text at the bottom |
| `toggle` | `false` | For dialogs bound to a key (a launcher, a menu): pressing the key again **closes** the dialog instead of opening another, and repeated or held-down presses never pile up in the queue. Matched by config file; the repeat exits with status 1 |
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
at `inside`, and nothing under the pointer is highlighted or selected, until the
pointer actually moves again (or is clicked or scrolled). Each dialog starts in
that state.

```toml
[opacity]
inside     = 100
outside    = 55
transition = 250
```

## `[back]`

A dialog can offer **Back**: a `‹ Back` button in a header row next to the
title, Backspace on tiles, a list or an empty search box, and Alt+Left
anywhere. Back ends the dialog with exit status **4**, prints nothing, and runs
the command in `SPOT_BACK` if there is one.

Whether to offer it is the *caller's* call, not the config's: only the caller
knows if there is somewhere to go back to. Nothing is shown unless one of these
is set, so the same dialog has Back when a menu opened it and none when a
keybind did:

| Caller says | Effect |
|-------------|--------|
| `spot --back cfg.toml` | Show Back; status 4 on use. For scripts that handle navigation themselves |
| `SPOT_BACK='arch-menu system' spot cfg.toml` | Show Back, **and** run that command when it is used |

`SPOT_BACK` is an environment variable, so it is inherited: a menu that
launches a script with it set gives every dialog that script opens a Back that
returns to the menu. The command runs with the caller's environment minus
`SPOT_BACK` itself, so the dialog it opens does not offer a Back of its own to
where it already is.

`[back]` only tunes the control:

| Key | Default | Description |
|-----|---------|-------------|
| `label` | `‹ Back` | Button text |
| `show` | `auto` | `auto` shows it when the caller asked; `never` refuses, whatever the caller says (a top-level menu, say) |

```toml
[back]
label = "‹ Settings"
```

Backspace only goes back when it could not be deleting text: on tiles, a list,
or a search box that is already empty. In a field, password or text area it
stays an ordinary edit key. Esc is unchanged and still closes the dialog (exit
status 1, and no command is run).

## Items

### Common keys

| Key | Applies to | Description |
|-----|------------|-------------|
| `type` | all | `field`, `password`, `text`, `select`, `list`, `apps`, `tiles`, `toggle`, `scale`, `switch`, `segmented`, `button`, `separator`, `meter`. An unknown type is skipped with a log line |
| `id` | all | Name used in `{id}` templates and as the JSON key |
| `label` | all | Shown above the widget. For `toggle`, `switch` and `button` it is the text of the control; for `tiles` it is a section heading |
| `placeholder` | field, password, text, list | Greyed-out hint text |
| `query` | list, apps | Text the search box starts with (`SPOT_QUERY` overrides it) |
| `hide_empty` | list, apps | Hide the results list while it has no rows: just a search bar until you type something that matches (a Spotlight / run-dialog look) |
| `default` | field, text, select, list, tiles, toggle, scale, switch, segmented | Initial value. For `tiles` it is a tile `value`; for `toggle` `true` or `false`; for `scale` a number; for a multi `list` the options joined by `separator` |
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
| `collapsible` | `false` | Group the options under headers that fold and unfold; see [Collapsible lists](#collapsible-lists) |
| `collapsed` | `true` | (collapsible) Groups start folded |
| `group_separator` | tab | (collapsible) What splits `GROUP` from `ROW` in an option |

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

### Collapsible lists

`collapsible = true` turns a `list` into a tree of folding groups, made for help
screens and long reference tables. Every option line is `GROUP<TAB>ROW`; lines
without the tab belong to no group and stay at the top. Groups appear in the
order they first occur, each as a header with a row count:

```
▸ Hyprland  (160)
▾ Yazi  (130)
    j       Move down
    k       Move up
▸ mpv  (190)
```

| Do | Effect |
|----|--------|
| Enter, click or double-click a header | Fold / unfold that group |
| Right / Left on a header (search empty) | Unfold / fold it |
| Left on a row (search empty) | Fold its group and land on its header |
| Type | Searches **inside folded groups too**: every group with a match opens, showing only the matches. The group name is part of what a row matches, so `yazi seek` finds the rows of the Yazi group. Clear the search and the folds come back |
| Enter on a row | Submits it: `{id}` and `stdout` get the row **without** its group |

```toml
[[items]]
type        = "list"
id          = "key"
collapsible = true
collapsed   = true          # start with every group folded (the default)
match       = "smart"
stdin       = true
```

```sh
printf 'Yazi\tj  move down\nYazi\tk  move up\nmpv\tSPACE  pause\n' | spot help.toml
```

Style the lines with `.spot-group-header` and `.spot-group-row` (see
[css.md](css.md)). `multi` and `collapsible` cannot be combined. Example:
[`examples/help.toml`](../examples/help.toml).

### `apps`

> A step-by-step guide with recipes is in [`launcher.md`](launcher.md); this section is the reference.

An application launcher: the installed applications (freedesktop `.desktop`
entries) as a searchable list; Enter **launches the selected one** and closes.
The index is cached by the daemon and rebuilt only when an applications
directory changes. It is the building block for a launcher menu in any style:
what is scanned, how apps are started and how it looks are all configuration
([`[apps]`](#apps-1) + CSS), none of it Go.

| Key | Default | Description |
|-----|---------|-------------|
| `placeholder` | none | Hint text in the search box |
| `height` | `320` | Minimum list height in px |
| `match` | `smart` | Matching mode (see `list`); `apps` is always ranked |

Icons come from the icon theme by name (`Icon=firefox`) or from an absolute file
path in the entry. The flatpak icon directories, `~/.local/share/icons` and the
`icons` folder next to each scanned applications folder are searched even when the
session's `XDG_DATA_DIRS` does not list them; an icon that cannot be found falls back
to a generic application icon. Style the rows with `.spot-app`, `.spot-app-icon`,
`.spot-app-name` and `.spot-app-comment` (see [css.md](css.md)).

The search matches the name first, then the generic name, comment, keywords and
desktop id (a match there ranks below a name match). With no query the list is
ordered by how often and how recently you launched things (`frecency`), then by
name. The item's value is the **desktop id** (`org.mozilla.firefox`), so
`submit.exec` / `submit.stdout` can use `{app}` (the item id) instead of the
default launch.

```toml
[[items]]
type        = "apps"
id          = "app"
placeholder = "Search apps…"
```

### `tiles`

A grid of large icon-and-label buttons. Clicking a tile, or pressing Enter on
it, picks it **and submits the whole dialog**, unless the tile [switches something](#tiles-that-switch).

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
| `class` | CSS classes for this tile. Every tile also has `spot-value-<value>` (other characters as `-`) |
| `state`, `on_change` | See [tiles that switch](#tiles-that-switch) |
| `from`, `call` | The same with a mod |

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

#### Tiles that switch

A tile with a `state` shows whether something is on, for a grid of quick settings:

```toml
[[items.tiles]]
label     = "Waybar"
value     = "waybar"
state     = "my-settings get waybar"             # prints on or off
on_change = "my-settings set waybar {value}"     # {value} is on or off
```

- Picking it **flips it and keeps the dialog open**; the states are then all read again.
- It shows "On" or "Off" and has `.spot-on` while on, `.spot-busy` while changing.
- While the state is **unknown** (not read yet, the command failed or printed something else) it has
  `.spot-unknown`, and picking it asks again instead of guessing. An old state is never kept on show.
- `on_change` should return only once the change has happened; the state is read right after.
- With `state` alone the tile only shows: picking it submits like any tile.
- `from = "mod.value"` and `call = "mod.request"` do the same with a [mod](modding.md); `{value}` goes
  to the request as its argument.
- `timeout` on the item bounds the commands (ms, default 10000).

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

### Live controls

`switch`, `segmented`, `scale` and list rows with switches can **apply at once**, with the dialog
staying open. `toggle` is different: it only changes what is submitted.

| Key | Description |
|-----|-------------|
| `state` | Command that prints the current value. Run when the dialog opens and again after every change made in it (of any control: one setting may change another) |
| `on_change` | Command that applies a new value. `{value}`, `{id}` and `{key}` are shell-quoted; the value is also in `$SPOT_VALUE` |
| `timeout` | ms a command may take before it is killed and reported (default `10000`) |

- Commands run with `sh -c` in the caller's environment and directory, off the UI thread.
- The value is the **last line** printed. For a switch, `on` / `off`, `true` / `false`, `1` / `0`,
  `yes` / `no` and the item's own `on` / `off` all count.
- If `on_change` fails (non-zero exit), the control goes back to what it showed and the error
  output appears under the dialog.
- Without `state`, `on_change` may print the value it settled on.
- A control keeps its value for `[submit]`, if there is one. Enter on a focused switch, segment or
  button activates it rather than submitting; Up and Down move between controls (not off a slider).
- A change made elsewhere while the dialog is open is not noticed until the next change in it.

Complete examples: [`settings.toml`](../examples/settings.toml), [`switches.toml`](../examples/switches.toml).

### `switch`

| Key | Default | Description |
|-----|---------|-------------|
| `look` | `"switch"` | `"switch"` (a sliding switch after the label), `"button"` (a button that stays pressed while on) or `"checkbox"` |
| `on`, `off` | `"true"`, `"false"` | Value for each position |
| `default` | `false` | Position until `state` has been read |

```toml
[[items]]
type      = "switch"
id        = "dnd"
label     = "Do not disturb"
look      = "button"
state     = "my-settings get dnd"
on_change = "my-settings set dnd {value}"
```

### `segmented`

A few options side by side, one of them chosen.

| Key | Description |
|-----|-------------|
| `options` | Values (required) |
| `labels` | What each shows, as many as `options` (default: the value) |

### `button`

| Key | Default | Description |
|-----|---------|-------------|
| `label` | required | Its text |
| `exec` | required | The command |
| `close` | `false` | `true`: start the command detached and close the dialog (status 0, nothing printed). `false`: wait for it, keep the dialog and read every state again |
| `timeout` | `10000` | ms, only for `close = false` |

### `separator`

A line between groups of items, under its `label` if it has one.

### `meter`

A bar that shows a number without changing it: battery, disk use, progress.

```toml
[[items]]
type   = "meter"
label  = "Battery"
state  = "cat /sys/class/power_supply/BAT0/capacity"
format = "{value} %"
```

| Key | Default | |
|-----|---------|---|
| `state`, or `from` | one is needed | A command printing the number (a trailing `%` is fine), or a mod's value |
| `min`, `max` | `0`, `100` | The ends of the bar; a number outside is shown as it is, with the bar full or empty |
| `format` | `{value}` | The text beside the bar |

It is read when the dialog opens and after every change made in it, not on a timer. A value that
cannot be read shows `…`. Classes: `.spot-meter-row`, `.spot-meter` (a `progressbar`),
`.spot-meter-text`, `.spot-unknown`.

### Lists with switches

`switches = true` on a plain `list` takes rows `LABEL<TAB>KEY<TAB>STATE`. A row whose state is on or
off shows a switch: Enter or a click flips it and the list stays open, with the search and the
selection as they were. A row without a state is an ordinary choice; `{id}` is the selected row's
`KEY` (the label when there is none).

`on_change` gets the row's `{key}` and the new `{value}`. `state`, if set, prints `KEY<TAB>on|off`
lines for the rows to follow. Not with `multi` or `collapsible`.

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

## `[apps]`

Configures the [`apps`](#apps) item. Every key is optional.

| Key | Default | Description |
|-----|---------|-------------|
| `paths` | XDG data dirs + flatpak exports | Directories scanned for `.desktop` files (`~` works). Earlier directories win when two hold the same desktop id |
| `exclude` | none | Globs matched against the desktop id **or** the name, case-insensitive: `["*Setup*", "org.kde.kcm_*"]` |
| `desktop` | `$XDG_CURRENT_DESKTOP` | Desktop names that `OnlyShowIn` / `NotShowIn` are checked against |
| `icons` | `true` | Show each app's icon: a row is then icon, name and the comment under it. `false` gives plain `Name  —  comment` lines |
| `icon_size` | `32` | Icon size in px |
| `icon_size_selected` | `icon_size` | Icon size of the selected row, so the highlighted app can be larger than the rest (the text size is CSS: `.spot-list row:selected .spot-app-name`) |
| `show_comment` | `true` | Show the comment (under the name, or after it without icons) |
| `max_results` | `0` | Cap on the list (`0` = all) |
| `frecency` | `true` | Rank by launch count and recency, kept in `$XDG_STATE_HOME/spot/frecency.json` |

Hidden entries (`NoDisplay`, `Hidden`), entries whose `TryExec` is missing and
entries for other desktops are never shown. The default `paths` include the
flatpak export directories even when the session `XDG_DATA_DIRS` lacks them.

### `[apps.launch]`

How an app is started. It is **not** GIO's launcher: spot runs the command you
give, so `uwsm-app`, `systemd-run` or any wrapper stays under your control, and
the app starts detached (its own session, no stdio) so it outlives the dialog.

| Key | Default | Description |
|-----|---------|-------------|
| `command` | `{exec}` | Command template. `{exec}` is the entry's `Exec` line, already resolved and shell-quoted: do not put quotes around it |
| `terminal` | `xdg-terminal-exec {exec}` | Wraps apps with `Terminal=true`; its result becomes the `{exec}` of `command` |
| `cwd` | the entry's `Path=`, else the caller's | Working directory (`~` works) |
| `env` | none | Extra environment, a table of `NAME = "value"` |

The `Exec` field codes are resolved per the Desktop Entry spec: `%f %F %u %U`
take the [target](#opening-a-file-or-url) and vanish without one, `%i` becomes `--icon <icon>`, `%c` the
name, `%k` the `.desktop` path and `%%` a percent sign.

#### Opening a file or URL

`SPOT_TARGET` hands a file or URL to the application you pick:

```sh
SPOT_TARGET=~/notes.md spot launcher
SPOT_TARGET=https://example.org spot launcher
```

| In the entry's `Exec` | Gets |
|---|---|
| `%f`, `%F` | the absolute path (a `file://` URL is turned back into one; any other URL stays as it is) |
| `%u`, `%U` | a `file://` URI, percent-encoded; a URL stays as it is |
| neither | the path, as an extra last argument |

A relative path is relative to the directory `spot` was run in. It arrives as one argument whatever
characters it contains, provided the templates do not quote `{exec}`: inside `sh -c "{exec}"` a
file name could run commands. An override whose command has no `{exec}` gets no target. The
launched application does not inherit `SPOT_TARGET`. The list is not filtered by what can open the
target.

### `[[apps.override]]`

Replaces the whole command of one app, by desktop id. `{exec}` still works:

```toml
[apps.launch]
command = "uwsm-app -- {exec}"

[[apps.override]]
id      = "org.mozilla.firefox"
command = "uwsm-app -- firefox --new-window"
```

Example: [`examples/launcher.toml`](../examples/launcher.toml).

## `[[sources]]`

> The guide, with ready-made calculator, file, web-search and run sources, is [`launcher.md`](launcher.md#sources).

Prefixes that hand the launcher's search to a **command**. When the text in an
[`apps`](#apps) item starts with a source's prefix, the prefix is stripped, the
command runs for the rest (debounced, an older run is cancelled) and its output
replaces the app list. Each `[[sources]]` table is one source:

| Key | Default | Description |
|-----|---------|-------------|
| `name` | none | A label for messages, and `$SPOT_SOURCE` |
| `prefix` | required, unless `default` | What you type first: `=`, `.`, `@`, `>`. No whitespace. The longest matching prefix wins, so `=` and `==` can coexist |
| `default` | `false` | The source for everything typed **without** another source's prefix (at most one, and it takes no prefix). With a default source the dialog does not list apps at all: it is a bar that feeds your command, like a run dialog. See [`examples/run.toml`](../examples/run.toml) |
| `exec` | required | Run with `sh -c` in the caller's environment. The query is in `$SPOT_QUERY` and as `{query}` (shell-quoted). A non-zero exit is fine: whatever it printed is the answer |
| `activate` | print the value | Run when you press Enter on a row. `{value}`, `{label}` and `{query}` are shell-quoted. The command starts detached, then the dialog closes. Empty: the value is printed to stdout and the dialog closes |
| `icon` | none | Icon theme name shown on every row of this source (no icon column without it) |
| `delay` | `150` | Milliseconds to wait after the last keystroke before running |
| `min_chars` | `1` | Shortest query (without the prefix) that runs; below it the list is empty |
| `max_results` | `50` | Rows kept |
| `timeout` | `5000` | Milliseconds before the command is killed (its whole process group) |

The command prints one row per line: `label`, `label<TAB>value` or
`label<TAB>value<TAB>comment` (shown under the label). `value` defaults to the
label. Empty lines are skipped.

```toml
[[sources]]
name     = "calc"
prefix   = "="
exec     = "qalc -t -- {query} | head -n 1"       # "= 2+2" shows 4
activate = "wl-copy {value}"

[[sources]]
name     = "files"
prefix   = "."
exec     = "my-file-search {query}"               # prints  ~/docs/a.pdf<TAB>/home/me/docs/a.pdf
activate = "xdg-open {value}"
```

Sources work only on an `apps` item. To open the launcher already in a mode, set
`SPOT_QUERY`: `SPOT_QUERY=". " spot launcher`. Example:
[`examples/launcher.toml`](../examples/launcher.toml).

## Menus

A config with `[page.<name>]` tables is a **menu**: pages of entries in one window, with Back and a
search that reaches into the pages below. It has no `[[items]]` and no `[submit]`.

```toml
title  = "Menu"
width  = 320
style  = "spot.css"
toggle = true

[menu]
placeholder = "Search…"
height      = 480

[[page.main.entry]]
label = "Capture"
page  = "capture"

[[page.main.entry]]
label    = "Update"
terminal = "sudo pacman -Syu"
hold     = true

[page.capture]
title = "Capture"

[[page.capture.entry]]
label = "Region"
exec  = "hyprshot -m region"
```

| `[menu]` | Default | |
|----------|---------|---|
| `start` | `main` | The first page; its title is the config's `title` unless it has its own |
| `placeholder` | | Text of the empty search box |
| `height` | `320` | Height of the list, px |
| `terminal` | `xdg-terminal-exec` | The command that opens a terminal; spot adds `sh -c <command>` |
| `columns` | `1` | Show pages as a grid of tiles this many wide (up to 12); `1` is a list. A page can set its own |

A page has a `title` (default: its name) and one or more `[[page.<name>.entry]]`. An entry has a
`label` and **exactly one** of:

| Key | Does |
|-----|------|
| `page` | Goes to that page |
| `exec` | Closes the menu and runs a shell command |
| `terminal` | Closes the menu and runs a shell command in a terminal. With `hold = true` the terminal waits for Enter before closing |
| `open` | Closes the menu and opens a file or URL (`xdg-open`; no shell, `~` is not expanded) |

With `back = true`, an `exec` entry's command gets `SPOT_BACK`, so a dialog it opens has a Back to
this page, and the menu's window is kept for it.

**As a grid.** With `columns`, a page shows tiles: the glyph in front of a label large, the rest
below. The arrows move around it (Left and Right once the search is empty), and typing turns it into
the list, so that search results keep their way. Style it like [`tiles`](#tiles): `.spot-tile`,
`.spot-tile-icon`, `.spot-tile-label`, with `.spot-cursor` on the chosen one and `.spot-menu-grid` on
the grid. Make the window wide enough (`width`).

**Keys:** Enter picks; Back is Backspace on an empty search, Alt+Left or the button, and lands on the
entry you came through; Esc closes. A double click picks.

**Search** ([smart matching](#list)) shows the matches on the page first, then those on pages below
it, with the way to them ("Install › Terminal › Kitty"). Words of the way match too.

**Opening a page:** `spot --page setup menu.toml`. Back then goes up as if you had walked there.

**Includes:** `include = ["menu/*.toml"]` reads more pages from files below the config's folder
(links that lead outside it are refused). An included file holds `[page.*]` tables only, and a page
is defined once.

**A menu is a program:** its commands run as you, with no check. Open only menus you would run as a
script.

### Pages fed by mods

A page can take its rows from a [mod](modding.md). They come before the entries written on it.

```toml
[page.sockets]
from = "sockets.table"                 # a provider's value
exec = 'wl-copy -- "$SPOT_VALUE"'      # what picking any of its rows does

[page.remote]
run   = "cat ~/.ssh/config"            # your command, run as you
parse = "ssh-groups"                   # a parser mod turns its output into rows
then  = "hosts"                        # picking a row goes there

[page.hosts]
run   = "cat ~/.ssh/config"
parse = "ssh-hosts"
group = true                           # only rows whose `group` is the key just picked
exec  = 'xdg-terminal-exec ssh "$SPOT_VALUE"'
```

| Key | |
|-----|---|
| `from`, or `run` and `parse` | Where the rows come from |
| `then`, `exec`, `terminal` or `call` | **Exactly one:** go to a page; run a command; run it in a terminal (`hold` works); make a mod's request with the row's key |
| `group` | Show only the rows whose `group` is the key picked on the page before |
| `allow_leading_dash` | Let a key that starts with `-` reach the action |

**A row never becomes part of a command.** It is in `$SPOT_VALUE` (its key) and `$SPOT_LABEL`; write
them in double quotes. A key starting with `-` is refused unless the page allows it.

Rows are fetched when the page is first opened, once per menu. While they are on the way the page is
empty. If they cannot be had, the page says why and its written entries still work. The search covers
a fed page's rows only while you are on it.

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
contain `{other}` is not expanded again.

**`{id}` is pasted into the command as it is, not quoted.** A value holding `$(…)`
or `;` runs. Use `{id}` for `stdout`; in `exec` read the value from the
environment instead:

| Variable | Holds |
|----------|-------|
| `SPOT_VALUE_<ID>` | the value of the item `id` (upper-cased, other characters as `_`) |
| `SPOT_ACTIVE` | what `{_active}` would be |

```toml
[submit]
exec = 'ping -c 1 -- "$SPOT_VALUE_HOST"'    # quotes around it, "--" before it
```

Quoted like this nothing in a value can run. `--` keeps a value starting with `-`
from being read as an option, where the program supports it. This matters most for
values you did not type yourself, such as file or network names. For rows from `stdin`, and from a mod
or parser, `{id}` in `exec` is refused.

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
| `SPOT_PLACEHOLDER` | Placeholder of the first field, password, text, list or apps item |
| `SPOT_QUERY` | Text the search box of the first list or apps item starts with: `SPOT_QUERY="= " spot launcher` opens the launcher already in calculator mode |
| `SPOT_TARGET` | A file or URL to open with the application picked in an `apps` item: [details](#opening-a-file-or-url) |
| `SPOT_DEFAULT` | Default of the first item that takes one (field, text, select, list, tiles, toggle, scale, switch, segmented) |
| `SPOT_CURRENT_<ID>` | `current` of the `tiles` item with that `id`, same naming as `SPOT_DEFAULT_<ID>` |
| `SPOT_BACK` | Show [Back](#back) and run this command when it is used. Empty means unset |
| `SPOT_DEFAULT_<ID>` | Default of the item with that `id`, upper-cased with non-alphanumerics as `_` (`battery-mode` becomes `SPOT_DEFAULT_BATTERY_MODE`). Wins over `SPOT_DEFAULT` |

```sh
SPOT_TITLE="Delete 3 files?" SPOT_DEFAULT=no spot confirm.toml
SPOT_DEFAULT_BATTERY=balanced spot power.toml
```
