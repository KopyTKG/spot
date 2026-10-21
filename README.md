# spot

Transactional GUI dialog runtime for Wayland. Write a TOML config describing a
window (title, fields, footer, submit command), point it at a CSS file for
styling, and `spot` renders it as a layer-shell overlay — Enter submits, Esc
cancels.

Fills a gap between `walker --dmenu` (launcher-shaped, compiled-in templates),
`yad` / `zenity` (transactional but preset dialogs, no CSS), and `eww`
(declarative + CSS but daemon-based, built for ambient widgets, not one-shots).

**Status:** early / v0. Field, password, and select widgets only.

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
| `anchor`   | `center`      | `center` \| `top` \| `bottom` \| `left` \| `right` (space-separated to combine) |
| `width`    | natural       | Default window width in px                               |
| `height`   | natural       | Default window height in px                              |
| `style`    | —             | Path to CSS file, relative to this TOML                  |
| `footer`   | —             | Footer label text                                        |
| `keyboard` | `exclusive`   | Layer-shell keyboard mode: `exclusive` \| `on-demand` \| `none` |

### `[opacity]` (optional)

The layer surface covers the whole screen; a click outside the dialog closes it.
`[opacity]` fades the dialog itself depending on where the pointer is. Without
the table the dialog is always fully opaque.

| Key          | Default | Description                                              |
|--------------|---------|----------------------------------------------------------|
| `inside`     | `100`   | Dialog opacity in % (0-100) while the pointer is over it |
| `outside`    | `100`   | Dialog opacity in % while the pointer is outside it      |
| `transition` | `150`   | Fade time in ms (`0` = instant)                          |

The dialog starts at `inside` until the pointer first moves.

```toml
[opacity]
inside     = 100
outside    = 55
transition = 200
```

### `[[items]]`

| Key           | Applies to | Description                                                                 |
|---------------|------------|-----------------------------------------------------------------------------|
| `type`        | all        | `field` \| `password` \| `select`                                           |
| `id`          | all        | Identifier used for `{id}` substitution in `[submit].exec`                  |
| `label`       | all        | Label rendered above the widget                                             |
| `placeholder` | field      | Greyed-out placeholder text                                                 |
| `default`     | field      | Pre-filled value                                                            |
| `options`     | select     | Array of strings to populate the dropdown                                   |

### `[submit]`

| Key     | Default | Description                                                              |
|---------|---------|--------------------------------------------------------------------------|
| `exec`  | —       | Shell command template. `{id}` placeholders substituted from field values |
| `spawn` | `false` | If `true`, fire-and-forget then close window                              |

## CSS hooks

Layout is decided by widget choice in the TOML (GTK4 CSS has no flex/grid —
only visual styling). The runtime adds these classes; style them however you
want:

| Class           | Widget                                  |
|-----------------|-----------------------------------------|
| `.spot-window`  | Outer `GtkApplicationWindow`            |
| `.spot-root`    | Vertical box wrapping everything        |
| `.spot-title`   | Title label                             |
| `.spot-item`    | Per-item container (label + widget)     |
| `.spot-label`   | Item label                              |
| `.spot-field`   | Text input (`GtkEntry`)                 |
| `.spot-password`| Password input (`GtkPasswordEntry`)     |
| `.spot-select`  | Dropdown (`GtkDropDown`)                |
| `.spot-footer`  | Footer label                            |

## Build

Requires Go ≥ 1.21, GTK4 dev headers, and the gtk4-layer-shell C library.

```sh
# Arch
sudo pacman -S gtk4 gtk4-layer-shell go

go build -o spot .
```

## License

[MIT](LICENSE)
