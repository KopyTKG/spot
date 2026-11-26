# Building a launcher with spot

spot can be an application launcher, a run bar, a calculator, a file finder, an emoji
picker: anything shaped like *a search box above a list of results*. None of it is Go code
you write. You describe it in TOML, style it with CSS, and bind it to a key.

This page is the guide. Every key is listed in [`toml.md`](toml.md); the CSS classes are in
[`css.md`](css.md).

## Contents

- [What you get](#what-you-get)
- [A minimal launcher](#a-minimal-launcher)
- [Apps: where they come from, how they start](#apps-where-they-come-from-how-they-start)
- [Sources: a prefix hands the search to a command](#sources)
- [Recipes](#recipes): calculator, files, web search, run command
- [A run bar](#a-run-bar)
- [Binding it to a key](#binding-it-to-a-key)
- [Where it appears](#where-it-appears)
- [Styling the rows](#styling-the-rows)
- [Beyond apps](#beyond-apps): emoji, clipboard history
- [Troubleshooting](#troubleshooting)

## What you get

- **An `apps` list.** The installed applications (freedesktop `.desktop` entries, flatpaks
  included) with icons, a name and a comment, searched with spot's smart matching
  (`ff` finds Firefox, `sf` System Firewall). The most-launched apps come first.
- **Launching under your control.** Apps start through a command template, so `uwsm-app`,
  `systemd-run` or any wrapper is a line of config. Apps start detached and outlive the dialog.
- **Sources.** A prefix (`=`, `.`, `@`, `>`) hands the search to *a command of yours*, which
  prints rows; Enter runs another command on the selected row.
- **A bare search bar** (`hide_empty`) for run dialogs and spotlight-style looks.
- **Everything visual in CSS**, plus margins and offsets to place it anywhere.
- **Warm start.** The daemon keeps the app index, so the launcher opens at once.

## A minimal launcher

```toml
# ~/.config/spot/launcher.toml
title    = ""                 # no heading: just a search box and the list
width    = 640
style    = "launcher.css"     # relative to this file
toggle   = true               # the key again closes it

[[items]]
type        = "apps"
id          = "app"
placeholder = "Search apps…"

[apps.launch]
command = "uwsm-app -- {exec}"   # how an app is started; the default is just {exec}
```

```sh
spot launcher        # looks up ~/.config/spot/launcher.toml
```

Type, arrow keys, Enter. Esc, or a click outside, closes it.

## Apps: where they come from, how they start

**What is listed.** Every `.desktop` file in the XDG data directories plus the flatpak export
directories (`/var/lib/flatpak/exports/share`, `~/.local/share/flatpak/exports/share`), even
when the session's `XDG_DATA_DIRS` lacks them. Entries are hidden when they are `NoDisplay` /
`Hidden`, belong to another desktop (`OnlyShowIn` / `NotShowIn`), name a `TryExec` that is
missing, or match your `exclude` globs. Set `paths` to scan other folders.

**How it is found.** The index lives in the daemon and is rebuilt when an applications
folder gains or loses a file (a new app or webapp shows up by itself). Editing an existing
`.desktop` file in place is not noticed until the folder changes; `spot --quit` forces a rescan.

**How it is ranked.** The name first, then the generic name, comment, keywords and desktop
id (a match there ranks below a name match). With nothing typed: most-launched first, then by
name. The launch counts live in `$XDG_STATE_HOME/spot/frecency.json` (`frecency = false` turns
it off).

**How it starts.** `[apps.launch]`:

| Key | What for |
|-----|----------|
| `command` | Template; `{exec}` is the entry's `Exec` line with its field codes resolved and shell-quoted (so do not put quotes around it): `uwsm-app -- {exec}` |
| `terminal` | Wraps apps that declare `Terminal=true`; its result becomes the `{exec}` of `command`: `xdg-terminal-exec {exec}` |
| `cwd`, `env` | Working directory and extra environment |

Per-app exceptions go in `[[apps.override]]` (`id` + a whole `command`). Field codes follow the
Desktop Entry spec: `%f %F %u %U` take the file or URL in `SPOT_TARGET`
([details](toml.md#opening-a-file-or-url)) and vanish without one, `%i` becomes
`--icon <Icon>`, `%c` the name, `%k` the file path.

## Sources

A source is *a command that fills the list while you type*. Declare one per prefix:

```toml
[[sources]]
name     = "calc"
prefix   = "="                     # type "= 2+2"
icon     = "accessories-calculator"
exec     = "calc.sh {query}"       # prints rows
activate = "wl-copy {value}"       # runs on Enter
```

- When the text starts with `=`, the prefix is dropped and `exec` runs for the rest, about
  150 ms after your last keystroke. If you type again before it finishes, the old run is
  cancelled; a run that takes longer than `timeout` (5 s) is killed together with everything it
  started. The rows replace the app list. Delete the prefix and the apps come back.
- **Rows** are lines of `label`, `label<TAB>value` or `label<TAB>value<TAB>comment`. The label
  is shown, the comment goes under it, the value (default: the label) is what `activate` gets.
- **`activate`** may use `{value}`, `{label}` and `{query}`; each is shell-quoted, so what you
  typed can never break out of the command. They are also in `$SPOT_VALUE`, `$SPOT_LABEL` and
  `$SPOT_QUERY`. The command starts detached and the dialog closes. Without `activate`, the value
  is printed to stdout instead (for scripts that call spot).
- **`exec`** runs with `sh -c` in *your* environment (the caller's, not the daemon's), with the
  query in `{query}` and `$SPOT_QUERY`. Whatever it prints is the answer, even when it exits
  non-zero (`grep` with no match exits 1).
- The longest matching prefix wins, so `=` and `==` can both exist. `min_chars`, `max_results`,
  `delay` and `timeout` tune a source; all keys are in [`toml.md`](toml.md#sources).
- A source with **`default = true`** takes everything typed without a prefix, and replaces
  the app list entirely: that is a run bar (below).

## Recipes

Four ready-to-use commands are in [`../examples/sources/`](../examples/sources/); put them on
your `PATH` and [`../examples/launcher.toml`](../examples/launcher.toml) works as it is.

| Type | Does | Command | Enter |
|------|------|---------|-------|
| `= 2+2` | Calculator: one row with the answer (`5 ft to cm` converts units) | `calc.sh` (qalc) | copies the result |
| `. report` | Files and folders under `~`, the folder shown as the comment | `files.sh` (fd) | opens it (`xdg-open`; a file manager for folders) |
| `@ gtk4` | One row per search engine | `web.sh` (jq) | opens the search |
| `> htop` | Runs what you typed; below it, programs on your `PATH` that complete the first word | `run.sh` | runs it |

Writing your own is the point. A source is any program that prints rows. This one lists
git branches for `g <text>`:

```toml
[[sources]]
name     = "branches"
prefix   = "g"
exec     = "git -C ~/code/myrepo branch --format='%(refname:short)' | grep -i -- {query}"
activate = "notify-send {value}"
```

Two notes on the run recipe. A plain `sh -c` has no terminal, so a terminal program such as
`htop` fails at once there. Either bind a second prefix that wraps the command in your terminal
(`activate = "xdg-terminal-exec sh -c {value}"`, as `>>` in `run.toml`), or point `activate` at a
small wrapper that retries in a terminal when the command fails straight away. And `sh -c` does
not see your shell's aliases or functions.

## A run bar

Alt+F2 style: only a search box, nothing under it until there is something to show.

```toml
# ~/.config/spot/run.toml
title    = ""
anchor   = "top"
margin_top = 160
width    = 640
toggle   = true

[[items]]
type        = "apps"
id          = "run"
placeholder = "Run…"
hide_empty  = true             # the list is hidden while it has no rows

[[sources]]
name     = "run"
default  = true                # no prefix: everything typed goes here
exec     = "run.sh {query}"
activate = "sh -c {value}"
```

`hide_empty` works on any list or `apps` item. Together with a `default` source the dialog is a
single input that feeds a command: a run bar, a calculator, a jump-to-project box.
Full file: [`../examples/run.toml`](../examples/run.toml).

## Binding it to a key

spot has no keybinding of its own; the compositor calls it.

```lua
-- Hyprland (Lua config)
hl.bind("SUPER + SPACE", hl.dsp.exec_cmd("spot launcher"))
hl.bind("ALT + F2",      hl.dsp.exec_cmd("spot run"))
```

```ini
# Hyprland (hyprlang) and sway
bind = SUPER, space, exec, spot launcher          # Hyprland
bindsym Mod4+space exec spot launcher             # sway
```

- **`toggle = true`** in the config makes the key a real toggle: pressing it while the dialog is
  open closes it, and held-down or repeated presses never queue up extra windows.
- **Open it in a mode** with `SPOT_QUERY`, which pre-fills the search box:
  `SPOT_QUERY="= " spot launcher` is a calculator key, `SPOT_QUERY=". " spot launcher` a file finder.
  (`query = "…"` in the config does the same.)
- **Close it from a script or another key:** `spot --dismiss`.
- Start the daemon at login so the first press is instant: see [`daemon.md`](daemon.md).

## Where it appears

By default a dialog is centred. Every dialog (a launcher included) can be placed from its
own config; all of these are optional:

| Key | Effect |
|-----|--------|
| `anchor` | `center` (default), `top`, `bottom`, `left`, `right`, or two of them: `top right` |
| `margin_top`, `margin_bottom`, `margin_left`, `margin_right` | Distance in px from the screen edge the anchor sits on |
| `offset_x`, `offset_y` | Shift in px from where the anchor puts it; **+ is right / down**, − left / up. Also works on a centred axis: `offset_y = -80` lifts a centred dialog 80 px above the middle |

```toml
anchor     = "top"
margin_top = 120              # a Spotlight-like bar near the top
```

```toml
offset_y = -80                # still centred, a little above the middle
```

## Styling the rows

The launcher is styled like every other dialog ([`css.md`](css.md)), with these classes for its
rows:

| Class | What |
|-------|------|
| `.spot-app` | One row (also `.spot-list-row`; `:selected` is on the parent `row`) |
| `.spot-app-icon` | The icon (its size is `icon_size` in `[apps]`, not CSS) |
| `.spot-app-text` | The box holding name and comment |
| `.spot-app-name`, `.spot-app-comment` | The two labels |

```css
.spot-app            { padding: 6px 12px; min-height: 48px; }
.spot-app-name       { font-size: 11pt; }
.spot-app-comment    { font-size: 9pt; opacity: 0.65; }

/* the selected row bigger: larger text here, a larger icon via icon_size_selected */
.spot-list row:selected .spot-app-name    { font-size: 13.5pt; font-weight: bold; }
.spot-list row:selected .spot-app-comment { font-size: 10pt; }
```

**Keep every row the same height.** If the selected row is taller than the others (extra
padding, say), every Up or Down press changes the height of two rows, the rows below move, and
the list scrolls after the highlight. Set a `min-height` that fits the selected row on *all*
rows and grow only the icon and the text.

For `icon_size_selected` the selected row's icon grows with the selection; `icons = false` drops
the icons for plain `Name — comment` lines. A complete theme is
[`../examples/theme.css`](../examples/theme.css). Blur behind the dialog is the compositor's job
(on Hyprland a `layer_rule` for the `spot` namespace), not GTK's.

## Beyond apps

The same pieces make other pickers. Two patterns:

- **Emoji picker.** A collapsible list (`collapsible = true`, `stdin = true`) fed one
  `Category<TAB>emoji  name` line per emoji gives one folded group per category that is searched
  all at once; the script copies the emoji and pastes it. Add each emoji's chat shortcodes to its
  line (`:rofl:`) and `rofl` finds it too.
- **Clipboard history.** A plain `list` fed by `cliphist list`; Enter decodes the entry back to
  the clipboard.

In both the *script* is the picker's logic and spot is only the window: spot reads rows from stdin and
prints the selection. See [`../examples/help.toml`](../examples/help.toml) and
[`../examples/menu.toml`](../examples/menu.toml).

## Troubleshooting

| Symptom | Check |
|---------|-------|
| A prefix does nothing | The command works by hand? Run it yourself with the query: `calc.sh 2+2`. It must print rows to **stdout** and be on the `PATH` of the shell that called `spot` (a keybind's environment, not your terminal's) |
| The list stays empty | `min_chars` (default 1) not reached; or the command printed nothing; or it timed out (`timeout`, default 5 s) |
| A new app is missing | New `.desktop` files are noticed automatically; an *edited* one is not until the folder changes. `spot --quit` rescans |
| Terminal apps flash and vanish | A plain `sh -c` has no terminal. Wrap the command in `xdg-terminal-exec` (see the run recipe) |
| Pressing the key twice stacks windows | Add `toggle = true` to the dialog's config |
| Icons are generic | The icon name is not in your icon theme; spot also searches the flatpak and `~/.local/share/icons` folders. A full path in `Icon=` works too |
| Config errors | `spot <file>` exits with status 3 and prints the reason on stderr (a `sources` entry without a prefix, a duplicate prefix, a negative margin) |
