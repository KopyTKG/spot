# Roadmap

Where spot is going, and what it is not. Shipped work is in the [changelog](../CHANGELOG.md).
Nothing below the Shipped table is released; [0.9 is built on a branch](#where-09-stands). The version
numbers show order, not dates.

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

- 0.9: how a mod host and an external backend (0.12) share one model, so there are not two systems.
- 0.10: whether a menu tree is one file or includes others.
- 0.11: how containers nest in TOML without becoming unreadable.
- 0.12: the backend protocol, its versioning, and what happens when the backend dies.

## Where 0.9 stands

As of 2026-11-27. **Not released.** The work is on the
[`mods` branch](https://github.com/KopyTKG/spot/tree/mods); installed spot has none of it.

**Built and tested**

- Mod hosts in a kernel sandbox (Landlock ABI 11 and seccomp), each in a `systemd` scope with memory
  and task limits. Without that kernel, no mod runs.
- Parser and provider mods; a scanner that refuses code hiding what it does and flags what deserves a
  look.
- Approval at a terminal, typing every program and every new or dangerous permission. Any change to a
  mod voids it.
- The daemon runs listed and approved mods: `spot get`, `watch` (also for waybar) and `call`, timers,
  notifications when a mod breaks and recovers, an audit log. Configured in `mods.toml`.
- Packaging of the mod host, and an [example](https://github.com/KopyTKG/spot/tree/mods/examples/mods):
  an `ss` parser and a sockets provider, with a searchable table. The table, approval and
  re-approval after a change have been used by hand.

**Before it is released**

- The verdict on the proof ([#24](https://github.com/KopyTKG/spot/issues/24)): is the mod clearly
  simpler than the script it replaces? If not, the API is revisited first. It has the same numbers in
  about 55 lines against 130, but cannot name processes or use root.
- A second person trying it, above all the approval screen.
- The bar counter in a real bar.
- Merging and tagging in one go: the `spot-bin` recipe expects a download that only a new release has.

**Not built, and 0.9 waits for it**

- Dialogs that read mods directly, `from` and `call`
  ([#23](https://github.com/KopyTKG/spot/issues/23)). Until then a script feeds the dialog.
- Security items of [#19](https://github.com/KopyTKG/spot/issues/19) and
  [#29](https://github.com/KopyTKG/spot/issues/29): a second way to stop a mod's detached programs,
  which matters without `systemd`; refusing to run with Linux capabilities; the daemon's socket in a
  folder of its own; a script of your own being swapped between its check and its start.
- An item-by-item check of #19 and #29 against the code and tests, which may add to that list.

**Not built, smaller**

- `spot parse`, and a log view per mod.
- Gaps in [#21](https://github.com/KopyTKG/spot/issues/21) and
  [#22](https://github.com/KopyTKG/spot/issues/22): a permission for private data, saying what is new
  in the notification.
- The sandbox is tested on Arch only, on `x86_64`. The CI machine cannot test it, so
  `just test-mods` runs it before a release.

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
