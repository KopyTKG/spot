# Roadmap

Where spot is going, and what it is not. Shipped work is in the [changelog](../CHANGELOG.md).
Nothing below the Shipped table exists yet, and the version numbers show order, not dates.

## Direction

spot is to become a **self-sufficient UI framework and runner for tools of any size**: whatever
someone wants to build for their desktop, spot can draw it and run it.

| Size | Examples | Today |
|------|----------|-------|
| Small | confirm, password, power menu, launcher | covered |
| Mid | settings, firewall, Wi-Fi picker, task list | worked around by chaining dialogs and shell |
| Big | a control centre, a file or service manager, a dashboard | not possible |

It started as a dialog runtime (ask, answer, done) and since 0.5 is also a framework for
launcher-shaped tools. Both stay; the rest builds on them.

## Principles

- **TOML describes, CSS styles, programs decide.** The interface is configuration, the look is a
  stylesheet, and logic lives in ordinary programs spot runs. TOML gets no conditions or loops.
- **Small stays small.** A confirm dialog needs no more lines than it does now, and existing
  configs keep working.
- **Any language.** A script is enough for a small tool; a bigger one is a program spot talks to.
- **Every step replaces something real.** A feature ships together with a tool that uses it.

## Foundations

What mid and big tools need, and widgets alone do not give:

1. **Dialogs that stay open:** actions run without closing.
2. **State and updates:** a control reads a value, changes it, and others refresh.
3. **Layout:** rows, columns, grids and split views, not only one vertical stack.
4. **Navigation:** pages, tabs and Back within one window.
5. **Live data:** lists and tables fed by commands that poll or stream.
6. **A window mode:** a normal window that can be moved, tiled and left open, beside the overlay.

## Plan

| Release | Adds | Proven by building |
|---------|------|--------------------|
| [0.9](https://github.com/KopyTKG/spot/milestone/1) | **Daemon mods** (experimental): parser and provider mods in embedded Lua, run in separate mod host processes; `spot get` / `watch` / `call` ([#16](https://github.com/KopyTKG/spot/issues/16), [draft](modding.md)) | a sockets mod feeding a table and a bar counter |
| [0.10](https://github.com/KopyTKG/spot/milestone/2) | Pages and menu trees, searchable across every level, fed by mods | a whole system menu in one config |
| [0.11](https://github.com/KopyTKG/spot/milestone/3) | Layout containers; live lists and tables; row actions | firewall and "Open With" tools |
| [0.12](https://github.com/KopyTKG/spot/milestone/4) | Window mode; a long-running backend exchanging JSON lines with the window | a small control centre |

Daemon mods come first because they make real tools much easier to build, and need no new layout or
widget: today's lists and controls can show their data. Their API stays experimental until menu
trees have used it.

**UI mods** (behaviour inside one dialog) come later, with no release set.

Open design questions, to settle before the release that needs them:

- 0.9: the mod API: publish and request, enabling, errors and reloading.
- 0.9: how a mod host and an external backend (0.12) share one model, so there are not two systems.
- 0.10: whether a menu tree is one file or includes others.
- 0.11: how containers nest in TOML without becoming unreadable.
- 0.12: the backend protocol, its versioning, and what happens when the backend dies.

## Also wanted

Not tied to a release above:

- **Left from 0.8** ([#18](https://github.com/KopyTKG/spot/issues/18)): a meter, switch state on
  tiles, per-tile CSS classes. States are read on open and after each change; noticing a change made
  elsewhere (polling or a signal) waits for live data in 0.11.
- **"Open With" by file type** ([#13](https://github.com/KopyTKG/spot/issues/13)); its extra keys
  should come from the general row actions.
- **A grid layout** (`layout = "grid"`, `columns`) for the apps list.
- **A filesystem watcher for the app index**, so an edited `.desktop` file shows at once.
- **Desktop actions** ("New Window", "New Private Window") as secondary entries of an app.
- **Result previews:** an image or text preview per row.
- **Confirm on a tile or row**, key shortcuts on tiles, icons on ordinary list rows,
  `spot --check` to validate a config.
- **More ready-made sources** in `examples/sources/`.

## Shipped

| Version | Launcher framework |
|---------|--------------------|
| 0.5 | `type = "apps"`: the `.desktop` index, field codes, launch templates, frecency, icons and a two-line row; collapsible lists; `toggle` for keybind dialogs |
| 0.6 | `[[sources]]`: a prefix hands the search to a command (calculator, files, web search, run) |
| 0.7 | Default sources and `hide_empty` (a bare run bar); per-dialog `margin_*` and `offset_*` placement |
| 0.7.1–0.7.3 | A hidden pointer no longer highlights; the daemon memory leak fixed; source split into `internal/` |
| 0.7.4 | `SPOT_TARGET`: open a file or URL with the picked app; `MimeType=` in the index |
| 0.8 | Controls that apply at once (`state` / `on_change`): `switch`, `segmented`, `button`, `separator`, live `scale`, switch rows in lists |

## Not planned

- **Another UI core.** spot stays on GTK4; web-based and lighter toolkits were weighed in
  [#14](https://github.com/KopyTKG/spot/issues/14).
- **Bars, panels and a notification daemon.** Always-visible desktop furniture pulls the design a
  different way, and Waybar, [eww](https://github.com/elkowar/eww) and mako already do it well.
- **Its own keybindings.** The compositor binds keys and calls `spot`; `toggle` and `SPOT_QUERY`
  make that comfortable.
- **Native plugins.** Mods are Lua run by spot, with limits so that a broken one cannot stall or
  crash it; sources and backends are separate programs in any language. Nothing is loaded as
  machine code.
- **A scripting language in TOML.**
- **Fuzzy matching beyond the smart matcher.** `match = "smart"` (prefixes, word initials,
  substrings, letters in order) is deliberately predictable; a source can rank its own output.
- **Clipboard or emoji built in.** They are good first examples of a *script feeding a list*
  ([`launcher.md`](launcher.md#beyond-apps)); the history and the emoji data belong to other tools.
