# Roadmap

Where spot is going, and what it is not. Shipped work is in the [changelog](../CHANGELOG.md).
Nothing in the plan after 0.9 exists yet, and the version numbers show order, not dates.

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
| [0.10](https://github.com/KopyTKG/spot/milestone/2) | Pages and menu trees, searchable across every level, fed by mods ([#25](https://github.com/KopyTKG/spot/issues/25)) | a whole system menu in one config |
| [0.11](https://github.com/KopyTKG/spot/milestone/5) | **Trust:** approvals only root can write, fingerprints for configs, CSS and scripts, a built-in manager ([draft](framework.md)) | a cloned setup reviewed and approved in one window |
| [0.12](https://github.com/KopyTKG/spot/milestone/6) | **Actions:** built-in actions for mods, root commands confirmed each time, unsandboxed mods as an alpha ([draft](framework.md)) | real shell scripts replaced |
| [0.13](https://github.com/KopyTKG/spot/milestone/3) | Layout containers; live lists and tables; row actions | firewall and "Open With" tools |
| [0.14](https://github.com/KopyTKG/spot/milestone/4) | Window mode; a long-running backend exchanging JSON lines with the window | a small control centre |

Daemon mods come first because they make real tools much easier to build, and need no new layout or
widget: today's lists and controls can show their data. Their API stays experimental until menu
trees have used it.

**UI mods** (behaviour inside one dialog) come later, with no release set.

Open design questions, to settle before the release that needs them:

- 0.11: what the root-owned helper reads and shows by itself; checking files without slowing menus.
- 0.13: how containers nest in TOML without becoming unreadable.
- 0.14: how a mod host and a backend share one model; the backend protocol, its versioning, and what
  happens when the backend dies.

## Where 0.9 stands

**Released as 0.9.0** on 2026-11-27, as experimental.

**Built and tested**

- Mod hosts in a kernel sandbox (Landlock ABI 11 and seccomp), each in a `systemd` scope with memory
  and task limits. Without that kernel, no mod runs.
- Parser and provider mods; a scanner that refuses code hiding what it does and flags what deserves a
  look.
- Approval at a terminal, typing every program and every new or dangerous permission. Any change to a
  mod voids it.
- The daemon runs listed and approved mods: `spot get`, `watch` (also for waybar) and `call`, timers,
  notifications when a mod breaks and recovers, an audit log. Configured in `mods.toml`.
- Dialogs use mods: `from` and `call` on controls and lists, `run` with `parse`, `submit.call` and
  `submit.copy`, so that a tool needs no shell. Also `spot parse`.
- Approval covers every file of a mod's folder, and names what in it would run without the sandbox.
- A log and an audit log per mod; the notification for a changed mod says what it newly asks for.
- Packaging of the mod host, and an [example](../examples/mods):
  an `ss` parser, a sockets provider and a searchable table, with no shell script. The table, copying,
  approval and re-approval after a change have been used by hand.

**What using it showed** (a sockets bar count, and a firewall tool with a parser and a provider)

- Verdict ([#24](https://github.com/KopyTKG/spot/issues/24)): a narrow pass. Mods pay off where several
  things need the same data, such as a bar and a dialog; the code is not smaller, and for a tool with
  one consumer a script stays simpler.
- It led to `spot get --fresh` and to icon-font characters passing `--waybar`.

**Still to find out**, with the API marked experimental until menu trees (0.10) have used it

- What a second person makes of it, above all of the approval screen.

**Known limits of 0.9**

- A script of your own can still be swapped between its check and its start.
- Without a `systemd` user session there are no memory or task limits.
- A name whose DNS answer needs TCP resolves only for a mod that also lists `53` in `net`.
- A list does not update while it is open (0.13).
- The sandbox needs Linux with Landlock ABI 11 on `x86_64`. It is tested on Arch, and in Debian and
  Fedora containers on the same kernel. The CI machine cannot test it, so `just test-mods` runs it
  before a release.

What else is left over is in [#32](https://github.com/KopyTKG/spot/issues/32).

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
| 0.9 | Daemon mods (experimental): sandboxed parsers and providers, approval, `spot get` / `watch` / `call` / `parse`, `from` / `call` in dialogs, `submit.copy` |

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
