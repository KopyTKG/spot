# Changelog

Releases are tagged `vX.Y.Z`; each has a GitHub release with the `spot-linux-x64` binary, and the
AUR packages `spot-bin` / `spot-git` follow. The reference for every key is in [`docs/`](docs/).

## Unreleased

- **The daemon no longer leaks memory.** Closed dialogs and their list rows are freed.

## 0.7.1 — 2026-11-25

- **A hidden pointer no longer highlights anything.** While the cursor is hidden (a dialog just
  opened, or the keyboard is in use), the tile, row or button under it gets no hover style, and
  collapsible lists no longer move their selection there.
- **Docs and examples overhaul** (no changes to spot itself). A rewritten README; a launcher guide
  ([`docs/launcher.md`](docs/launcher.md)); a guide to tying spot to the system
  ([`docs/os-integration.md`](docs/os-integration.md)); a roadmap replacing the old planning notes; this
  changelog; an examples index; runnable source scripts in `examples/sources/`; and `examples/os/`, shell
  scripts that connect spot to a desktop (power menu, clipboard history, emoji picker, sockets table,
  window switcher, a `sudo` / `ssh` askpass).

## 0.7.0 — 2026-11-24

- **Run bar.** `default = true` on a `[[sources]]` entry makes it take everything typed without
  a prefix and replaces the app list; `hide_empty = true` hides the results list while it has no rows.
  Together: a config that is just a search box (`examples/run.toml`).
- **Placement.** `margin_top`, `margin_bottom`, `margin_left`, `margin_right` and `offset_x`,
  `offset_y` position any dialog from its anchor. All optional; nothing set means centred as before.

## 0.6.0 — 2026-11-23

- **Dynamic sources.** `[[sources]]` on an `apps` item: a prefix (`=`, `.`, `@`, `>`) hands the
  search to a command that runs as you type (debounced, cancelled when the query moves on, killed as
  a process group on timeout). Rows are `label[<TAB>value[<TAB>comment]]`; Enter runs `activate` with
  `{value}`, `{label}`, `{query}` shell-quoted.
- `query` / `SPOT_QUERY` pre-fill the search box, so a keybind can open the launcher in a mode.

## 0.5.0 — 2026-11-23

- **The apps module.** `type = "apps"`: an application launcher built from the `.desktop` index
  (locale names, `OnlyShowIn` / `NotShowIn` / `TryExec`, exclude globs), `Exec` field codes per the
  Desktop Entry spec, launch templates (`[apps.launch]`, `[[apps.override]]`), detached launching,
  smart-match ranking plus frecency, a daemon-cached index, icons with a two-line row and
  `icon_size_selected`.
- **Collapsible lists.** `collapsible = true`: `GROUP<TAB>ROW` options fold under headers; typing
  searches inside folded groups.
- **`toggle = true`**: a dialog bound to a key closes when asked for again, and repeats never queue.
- **Daemon settings.** `daemon.toml` and a systemd user service ([`docs/daemon.md`](docs/daemon.md)).

## 0.4.0 — 2026-11-11

- **Daemon mode.** `spot <config>` is a thin client of `spot daemon`, which owns one layer-shell
  window; moving between dialogs swaps the content in place, so the fade no longer restarts.
  `--chain`, `--last`, `--sub`, `--dismiss`, `--quit`, `--status`, `--oneshot`.
- **Back.** A header button, Backspace on an empty search or on tiles, Alt+Left; `SPOT_BACK=<command>`,
  `--back`, exit status 4.

## 0.3.1 — 2026-11-09

- Dialogs open with the pointer hidden and ignored, so a dialog opened from the keyboard does not flash the cursor or the fade.

## 0.3.0 — 2026-10-22

- **Tiles:** one keyboard cursor across all tile sections, a pick marker, and a badge on the live (`current`) value.

## 0.2.x — 2026-10-21

- **0.2.2:** `match = "smart"` for lists (abbreviation matching); reference docs in `docs/`.
- **0.2.1:** fix the app id for config names that start with a digit; `SPOT_DEFAULT_<ID>`; a demo.
- **0.2.0:** new widgets (`text`, `toggle`, `scale`, multi-select `list`), validation (`required`,
  `pattern`, `error`), `[submit] format = "json"`, `--version` / `--help`, configs looked up by name in
  `$XDG_CONFIG_HOME/spot`, documented exit codes, `on_error = "show" | "exit"`, unit tests.

## 0.1.0 — 2026-10-21

- First release: TOML-described layer-shell dialogs with a CSS theme; `field`, `password`, `select`,
  `list` and `tiles`; `[submit]` with `exec`, `stdout` and `{_active}`; the `[opacity]` fade; a click
  outside closes the dialog; examples, the AUR packages and the release pipeline.
