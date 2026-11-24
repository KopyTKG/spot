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
at `inside` until the pointer actually moves again.

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
| `type` | all | `field`, `password`, `text`, `select`, `list`, `apps`, `tiles`, `toggle`, `scale`. An unknown type is skipped with a log line |
| `id` | all | Name used in `{id}` templates and as the JSON key |
| `label` | all | Shown above the widget. For `toggle` it is the checkbox text; for `tiles` it is a section heading |
| `placeholder` | field, password, text, list | Greyed-out hint text |
| `query` | list, apps | Text the search box starts with (`SPOT_QUERY` overrides it) |
| `hide_empty` | list, apps | Hide the results list while it has no rows: just a search bar until you type something that matches (a Spotlight / run-dialog look) |
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
| `command` | `{exec}` | Command template. `{exec}` is the entry's `Exec` line, already resolved and shell-quoted |
| `terminal` | `xdg-terminal-exec {exec}` | Wraps apps with `Terminal=true`; its result becomes the `{exec}` of `command` |
| `cwd` | the entry's `Path=`, else the caller's | Working directory (`~` works) |
| `env` | none | Extra environment, a table of `NAME = "value"` |

The `Exec` field codes are resolved per the Desktop Entry spec: `%f %F %u %U`
(files, URLs: there are none to hand over) and the deprecated codes disappear,
`%i` becomes `--icon <Icon>`, `%c` the name, `%k` the `.desktop` path, `%%` a
percent sign.

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
| `SPOT_PLACEHOLDER` | Placeholder of the first field, password, text, list or apps item |
| `SPOT_QUERY` | Text the search box of the first list or apps item starts with: `SPOT_QUERY="= " spot launcher` opens the launcher already in calculator mode |
| `SPOT_DEFAULT` | Default of the first item that takes one (field, text, select, list, tiles, toggle, scale) |
| `SPOT_CURRENT_<ID>` | `current` of the `tiles` item with that `id`, same naming as `SPOT_DEFAULT_<ID>` |
| `SPOT_BACK` | Show [Back](#back) and run this command when it is used. Empty means unset |
| `SPOT_DEFAULT_<ID>` | Default of the item with that `id`, upper-cased with non-alphanumerics as `_` (`battery-mode` becomes `SPOT_DEFAULT_BATTERY_MODE`). Wins over `SPOT_DEFAULT` |

```sh
SPOT_TITLE="Delete 3 files?" SPOT_DEFAULT=no spot confirm.toml
SPOT_DEFAULT_BATTERY=balanced spot power.toml
```
