# Roadmap

Where spot is going, and what it is not. Shipped work is in the [changelog](../CHANGELOG.md).

## Direction

spot started as a dialog runtime (ask, answer, done). Since 0.5 it is also a **framework for
launcher-shaped tools**: a search box over a list, where *what the list contains*, *what Enter
does* and *how it looks* are configuration. The goal is that one person's Spotlight-style
launcher and another's compact run bar are two config files, with no Go code and no separate
service behind them.

## Shipped

| Version | Launcher framework |
|---------|--------------------|
| 0.5 | `type = "apps"`: the `.desktop` index, field codes, launch templates, frecency, icons and a two-line row; collapsible lists; `toggle` for keybind dialogs |
| 0.6 | `[[sources]]`: a prefix hands the search to a command (calculator, files, web search, run) |
| 0.7 | Default sources and `hide_empty` (a bare run bar); per-dialog `margin_*` and `offset_*` placement |

## Next

In rough order:

1. **A grid layout** (`layout = "grid"`, `columns`, Launchpad-style icon grids) for the apps list.
   Row widgets already carry an icon, a name and a comment; the grid is a second layout over the
   same rows.
2. **A filesystem watcher for the app index.** Today the daemon rescans when an applications
   folder's modification time changes (an app installed or removed). Watching the folders would also
   catch an edited `.desktop` file and make the refresh instant.
3. **Desktop actions** ("New Window", "New Private Window") as secondary entries of an app (Tab
   or → on an app lists them).
4. **Result previews.** An image or a text preview per row, which clipboard history needs (a copied
   image shows as a thumbnail) and which also helps file search.
5. **More ready-made sources**, as scripts in `examples/sources/`: Unicode character search by name,
   symbols, bookmarks, window switcher. A source is any program that prints rows, so these need no
   changes to spot itself.
6. **Sources on other dialogs.** Sources attach to an `apps` item today. A plain `list` that is
   fed by a command as you type would make a picker without the app index.

## Not planned

- **Ambient widgets** (bars, clocks, panels). spot draws one dialog at a time and holds no state of
  its own; [eww](https://github.com/elkowar/eww) and Waybar already do the ambient job.
- **Its own keybindings.** The compositor binds keys and calls `spot`; `toggle` and `SPOT_QUERY`
  make that comfortable.
- **A plugin API.** Sources are commands, not code loaded into spot. That keeps a source free to
  be written in any language and unable to crash the window.
- **Fuzzy matching beyond the smart matcher.** `match = "smart"` (prefixes, word initials,
  substrings, letters in order) is deliberately predictable; a source can rank its own output.
- **Clipboard or emoji built in.** They are good first examples of a *script feeding a list*
  ([`launcher.md`](launcher.md#beyond-apps)); the history and the emoji data belong to other tools.
