# spot launcher (app menu): plan

Written 2026-11-11 as notes for the next session. Nothing here is built yet.

## Goal

A launcher built into spot that replaces **walker** (SUPER+SPACE) in the
hypr-setup. It has to be configurable enough that one person keeps today's
look (the stone pill) and another makes it look like Apple's Spotlight or
Launchpad, **without touching Go code**:

- **where it looks** for apps (paths),
- **how it launches** them (launch options),
- **how it looks** (full CSS control, plus layout options CSS cannot express).

## What we have to build on

- The daemon (v0.4.0): one window, dialogs swapped in place, so a **warm app
  index** is possible. The index can live in the daemon and be ready before
  the key is pressed.
- `list` items with `match = "smart"` (abbreviation matching), `--back` /
  `SPOT_BACK`, per-dialog CSS, `submit.exec` running off the main loop.
- Limits today: list rows are **text-only labels**; the list is static (stdin);
  no icons, no subtitle, no grid.

## Decisions already made

- Icons are wanted. "Like Apple's launch menu" means icons, so the earlier
  "is text-only OK?" question is answered: **no**.
- All of it must be configurable from TOML + CSS. Not hard-coded.

## Open questions (ask first thing)

1. "Setting path": does it mean **application directories to scan**, the
   **location of the config file**, or both? (Plan below assumes the first,
   with the usual `~/.config/spot/launcher.toml` for the second.)
2. New **item type** (`type = "apps"` inside a normal spot TOML), or a new
   top-level kind of config (`spot launcher`)? An item type reuses `title`,
   `style`, `[back]`, `[opacity]`, and keeps one config model. Leaning item type.
3. Launchpad-style **paging** (swipe/pages of icons), or one scrolling grid?
   Leaning scrolling grid first.
4. Which extra sources matter first: calc, emoji/symbols, web search, files,
   clipboard? (walker has all of them; see the parity list.)

## Config sketch (a proposal, nothing fixed)

```toml
# ~/.config/spot/launcher.toml
title    = ""                       # no heading: a bare search pill, Spotlight-style
anchor   = "top"
width    = 640
style    = "launcher.css"

[[items]]
type = "apps"
id   = "app"
placeholder = "Search apps…"

[apps]
paths    = ["~/.local/share/applications", "/usr/share/applications",
            "/var/lib/flatpak/exports/share/applications"]   # default: $XDG_DATA_DIRS
exclude  = ["*Setup*", "org.kde.kcm_*"]       # id / name globs
only_show_in = ["Hyprland", ""]              # honour OnlyShowIn / NotShowIn / NoDisplay
layout   = "list"                            # list | grid
columns  = 6                                 # grid only
icon_size = 32                               # px
show_comment = true                          # second line under the name
results  = "always"                          # always | on-type  (Spotlight: only once you type)
max_results = 8
match    = "smart"
frecency = true                              # rank by how often/recently launched

[apps.launch]
command  = "uwsm-app -- {exec}"              # {exec} = the Exec line with field codes resolved
terminal = "xdg-terminal-exec {exec}"        # for Terminal=true apps
cwd      = "~"
env      = { }                               # extra environment

[[apps.override]]                            # per app, by desktop id
id      = "org.mozilla.firefox"
command = "uwsm-app -- firefox --new-window {args}"

[back]
show = "never"
```

Notes on the sketch:

- Field codes (`%f %F %u %U %i %c %k`) must be handled per the Desktop Entry
  spec. `{exec}` is the *resolved* command, so users cannot get this wrong.
- **Desktop actions** (e.g. "New Window", "New Private Window"): show as
  secondary entries (Tab or → on an app to list them) or as extra result rows.
- Launching goes through the configured command, **not** GIO's launcher, so
  `uwsm-app` scoping and the daemon's environment stay under the user's control.
- Use `gio.AppInfo` / a hand-written `.desktop` parser for **metadata and
  icons**; decide in Phase 1 (GIO already does icon lookup and locale names).

## CSS contract (the "full customization" part)

Stable, documented class names, added to `docs/css.md`:

```
.spot-apps                       the whole results area
.spot-app                        one result (row or grid cell); :selected, :hover
  .spot-app-icon                 the image
  .spot-app-text
    .spot-app-name
    .spot-app-comment
.spot-app-action                 a secondary action row
```

- Layout options that CSS cannot express (`layout`, `columns`, `icon_size`,
  `results = on-type`) live in TOML; everything visual is CSS.
- Recommend `@define-color` for palettes so a retheme is a few lines.
- Ship three example themes in `examples/`:
  - `launcher-stone.css`: today's pill (the current hypr-setup look),
  - `launcher-spotlight.css`: Apple Spotlight (wide rounded bar, big font,
    results appear only on typing, soft shadow),
  - `launcher-launchpad.css`: Launchpad (full-screen anchor, icon grid, blurred
    backdrop).
- Blur is the compositor's job, not GTK's: document the Hyprland
  `layer_rule` for namespace `spot` (blur + ignore_alpha) next to the examples.

## Daemon work

- Build the index at daemon start; refresh with inotify on every `paths` dir
  (replaces `pkill -SIGUSR1 walker` in `bin/arch-webapp-remove`).
- Frecency store: `$XDG_STATE_HOME/spot/frecency.json`. Score =
  smart-match score x frecency boost (weights configurable).
- Keep the keyboard-release-on-close behaviour: launching an app must not
  swallow its first keystrokes.
- Cold start must stay fast with ~500 apps. Measure before and after.

## Phases

1. **Text launcher.** `type = "apps"`, `[apps]` paths / exclude / launch,
   index + inotify, frecency, smart match. Replaces SUPER+SPACE for apps.
   Acceptance: opens warm in < 50 ms, launches through `uwsm-app`, flatpak apps
   appear, Terminal=true apps work.
2. **Icons, subtitle, grid.** Row widget with icon + name + comment; `layout =
   "grid"`; the three example themes; docs/css.md. Acceptance: Spotlight and
   Launchpad themes reproducible with CSS + TOML only.
3. **Other sources.** calc (`=`, via `qalc`), symbols/emoji (`:`), web search
   (`@`), files (`.`), provider list (`/`). Each as a `[[sources]]` entry with
   a prefix, so users can add their own commands as sources.
4. **Clipboard history.** `cliphist` + `wl-paste --watch`, rows with preview.
   Images lose thumbnails unless Phase 2's icon widget can show them.
5. **Retire walker** in hypr-setup (checklist below).

## Walker parity checklist (what hypr-setup uses today)

| Walker feature | Where | Replaced by |
|---|---|---|
| App launcher, SUPER+SPACE | `hypr/defaults/bindings/utilities.lua` | Phase 1 |
| `arch-menu` "Apps" entry | `bin/arch-menu` | Phase 1 (`spot launcher.toml`) |
| Emoji picker, SUPER+CTRL+E | `utilities.lua` | Phase 3 |
| Clipboard manager, SUPER+CTRL+V | `hypr/defaults/bindings/clipboard.lua` | Phase 4 |
| `=` calc / `@` web / `.` files / `:` symbols | `walker/config.toml` prefixes | Phase 3 |
| SUPER+W dismiss | `bin/arch-killactive` (walker branch) | already `spot --dismiss` |
| Webapp index refresh | `bin/arch-webapp-remove` (SIGUSR1) | inotify (daemon work) |
| `arch-reload walker` | `bin/arch-reload`, `arch-menu` Reload/Setup | drop the target |
| elephant restart + flatpak XDG_DATA_DIRS | `hypr/defaults/autostart.lua`, `envs.lua` | keep the XDG_DATA_DIRS part (the scanner needs it), drop the restart |
| `walker/`, `elephant/` config dirs | `install.sh` link list | remove |
| layer rule `namespace = "walker"` | `hypr/defaults/apps.lua` | `spot` rule (see CSS blur note) |
| packages `walker`, `elephant*` | `bootstrap.sh`, installed system | remove (also the orphaned `elephant-*-debug`) |
| docs | README (arch-menu, helper table) | update |

## State at the end of today (2026-11-11)

- **spot v0.4.0 is released.** Tagged and pushed by Martin (16:23, on `14d15eb`,
  the head of `service`); Jenkins built it and published the GitHub release a
  minute later (asset `spot-linux-x64`). Our part is only commit, tag, push;
  Jenkins does the rest, and the AUR step follows afterwards.
- **`main` has not been updated.** The tag is not an ancestor of `main`
  (`origin/main` is still `95638b2`). There is no divergence, so
  `git switch main && git merge --ff-only service && git push` brings `main`
  level with the release. Do this before the AUR step and before starting new
  work: `spot-git` builds from `main`'s head, so until then it would ship old
  code under a 0.4.0 version, and the launcher should branch from released code.
- **AUR: prepared, not published.** `packaging/aur/*` are updated to 0.4.0 with
  real checksums (`just aur 0.4.0`, committed and pushed on `service`). Publishing
  (`just aur-publish 0.4.0`) needs the AUR SSH key; from the assistant's shell
  `ssh aur@aur.archlinux.org` was `Permission denied (publickey)`, so run it
  yourself after `ssh-add` (the key is probably `~/.ssh/devel/aur`).
- **hypr-setup is pushed** (menus use Back; Dolphin fix; fish recursive `cd` /
  `**` completion). To activate: install the release spot, run `install.sh`, and
  `sudo pacman -S --needed kimageformats kio-admin`.
- **Installed spot** (`/usr/bin/spot`) is still a local dev build from before the
  release, not v0.4.0. Replace it with the released binary or the AUR package.
- Not done anywhere: the keyboard paths of Back (Enter/Esc/click on the real
  window) have only been exercised by GTK-level tests, not by hand.
- This file is not shipped by the AUR packages (they install README, docs/, examples/).
