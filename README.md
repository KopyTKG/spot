# spot

Transactional GUI dialog runtime for Wayland. Write a TOML config describing a
window (title, fields, footer, submit command), point it at a CSS file for
styling, and `spot` renders it as a layer-shell overlay — Enter submits, Esc
cancels.

Fills a gap between `walker --dmenu` (launcher-shaped, compiled-in templates),
`yad` / `zenity` (transactional but preset dialogs, no CSS), and `eww`
(declarative + CSS but daemon-based, built for ambient widgets, not one-shots).

**Status:** early / v0. Widgets: field, password, select, list, tiles.

## Behaviour

- **Enter** submits, **Esc** cancels (exit status 1, nothing printed).
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
```

See `examples/` for the matching `ssh.css`.

## Config reference

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
| `type`        | all               | `field` \| `password` \| `select` \| `list` \| `tiles`                       |
| `id`          | all               | Identifier used for `{id}` substitution in `[submit]`                       |
| `label`       | all               | Label rendered above the widget (a section heading for `tiles`)             |
| `placeholder` | field, password, list | Greyed-out placeholder text                                             |
| `default`     | field, select, list, tiles | Pre-filled / pre-selected value (for `tiles`, a tile `value`)      |
| `options`     | select, list      | Array of strings to populate the dropdown / list                            |
| `stdin`       | list              | If `true`, read newline-delimited options from stdin (dmenu-style)          |
| `height`      | list              | Minimum list height in px                                                   |
| `columns`     | tiles             | Tiles per row (default `3`)                                                 |

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

Templates substitute `{id}` with that item's current value. `{_active}` expands
to the `id` of the tiles section last interacted with, so a menu with several
tile sections can tell which one was used:

```toml
[submit]
stdout = "{_active} {action} {battery}"
```

## Environment overrides

Lets one shared config be parameterised per invocation:

| Variable           | Effect                                                              |
|--------------------|---------------------------------------------------------------------|
| `SPOT_TITLE`       | Replaces `title`                                                    |
| `SPOT_FOOTER`      | Replaces `footer`                                                   |
| `SPOT_PLACEHOLDER` | Placeholder of the first field / password / list item               |
| `SPOT_DEFAULT`     | Default of the first field / select / list / tiles item             |

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
| `.spot-footer`  | Footer label                            |

## Install

From the AUR: `spot-bin` (prebuilt release binary) or `spot-git` (built from the
repository head). Releases are cut manually; see
[`packaging/aur/README.md`](packaging/aur/README.md).

## Build

Requires Go ≥ 1.21, GTK4 dev headers, and the gtk4-layer-shell C library.

```sh
# Arch
sudo pacman -S gtk4 gtk4-layer-shell go

go build -o spot .   # or: just build
```

## License

[MIT](LICENSE)
