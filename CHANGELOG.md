# Changelog

Releases are tagged `vX.Y.Z`; each has a GitHub release with the `spot-linux-x64` and `spot-mod-host-linux-x64` binaries, and the
AUR packages `spot-bin` / `spot-git` follow. The reference for every key is in [`docs/`](docs/).

## 0.10.0 — 2026-11-28

- **Menus:** a config with `[page.<name>]` tables is a tree of pages in one window, with Back that
  returns to the entry you came through, a search that reaches into the pages below, includes, and
  `spot --page <name>`. See [`docs/toml.md`](docs/toml.md#menus).
- **Menu pages fed by mods:** rows from a provider, or from a parser reading your command's output,
  with the action written once in the config. A row never becomes part of a command.
- Without a `systemd` user session, `spot mods check` reported the memory of the mod host's supervisor
  instead of the host's.
- **A config that may need a change:** `{id}` of a `stdin = true` list in `submit.exec` is now refused
  (0.9 warned). Write `"$SPOT_VALUE_<ID>"` instead.
- Without a `systemd` user session, `spot mods check` reported the memory of the wrong process.

## 0.9.3 — 2026-11-27

- `spot get --fresh`: fail, printing nothing, when only an old value of a mod can be had, so that a
  script cannot take it for the answer.

## 0.9.2 — 2026-11-27

- **Icons from a mod disappeared:** `spot get` and `watch`, with `--waybar` too, replaced or escaped the
  private-use characters that icon fonts such as Nerd Fonts are made of, so a bar module showed nothing.

## 0.9.1 — 2026-11-27

What was left over from 0.9 ([#32](https://github.com/KopyTKG/spot/issues/32)).

- **Mods on Fedora:** the sandbox now grants `/usr/lib64` and Fedora's certificate folders; HTTPS from a
  mod failed there.
- Without a `systemd` user session, programs that detach themselves are now stopped with their mod.
- A script of your own that a mod runs is checked again right before each run.
- Rows from a mod may have an `icon`, a name from the icon theme.
- The daemon serves at most 256 callers at once. A custom socket path in a folder that others can change
  (not yours or root's, or group or world writable without the sticky bit) is refused. The launch
  history file is not read through a symbolic link.
- Built with Go 1.26.10.

## 0.9.0 — 2026-11-27

**Daemon mods (experimental).** A mod is a small Lua program the daemon runs for you: it gets and
parses data once and hands it to dialogs, bars and scripts. Nothing runs until you list it and
approve it. See [`examples/mods`](examples/mods/) and [`docs/modding.md`](docs/modding.md); the API may
change until menu trees (0.10) have used it.

- **Two kinds:** parsers turn text into records and can touch nothing; providers publish values and
  answer requests under permissions that each state a reason.
- **Sandboxed by the kernel:** each mod runs in its own `spot-mod-host` process, under Landlock and a
  seccomp filter, with memory and task limits. Mods need Linux with Landlock ABI 11; without it no mod
  runs, and everything else works as before.
- **Approval** at a terminal, typing every program and every new or dangerous permission. A change to
  any file of a mod voids it. A scanner refuses code that hides what it does and flags what deserves a
  look; read the mod anyway.
- **Command line:** `spot get`, `spot watch` (also `--waybar`), `spot call`, `spot parse`, and
  `spot mods` (`allow`, `show`, `diff`, `revoke`, `check`, `log`, `audit`, `reload`). Configured in
  `mods.toml`.
- **Dialogs:** `from` and `call` on lists, `switch`, `segmented`, `scale` and `button`; `run` with
  `parse`.

Also for dialogs without mods:

- `[submit] copy` puts a value on the clipboard (needs `wl-clipboard`) with no shell; `call` and `args`
  make a mod's request instead of `exec`.
- Keys that nothing reads, such as a misspelling, are now warned about.
- A `stdin` list whose value `submit.exec` pastes unquoted is warned about; a later release will refuse
  it. Use `$SPOT_VALUE_<ID>`.
- A literal `\n` in a failure message no longer becomes a line break in its notification.
- `get`, `watch`, `call`, `parse` and `mods` are now commands: a config of such a name is opened by its
  path.

Packages now also install `spot-mod-host`.

## 0.8.5 — 2026-11-27

The rest of the audit ([#31](https://github.com/KopyTKG/spot/issues/31)).

- Choosing in an empty tile section no longer crashes the daemon.
- **No more freezes on awkward input:** thousands of identical switch rows took minutes to build, and
  a long repetitive row made every keystroke slow.
- Limits on stdin (8 MB), config files, waiting dialogs, requests read at once and image dimensions.
- A value holding a NUL byte stops the submit instead of leaving `SPOT_VALUE_<ID>` empty.
- The daemon itself refuses password dialogs; a process asking for a password writes no core dump.
- Only a real socket is removed at the socket path; `~name` working folders are no longer mangled;
  temporary and log files are opened safely; the examples quote `$XDG_RUNTIME_DIR`.

## 0.8.4 — 2026-11-27

Security, from an audit ([#31](https://github.com/KopyTKG/spot/issues/31)):

- **Passwords no longer pass through the daemon.** A dialog with a `password` item is shown by the
  calling process.
- **`SPOT_VALUE_<ID>`** hands values to `submit.exec` and `spawn` in the environment, where quoting
  works; `{id}` is still pasted unquoted. The examples use the variables.
- The daemon refuses to run as root; requests, command output, stylesheets and images are bounded in size; the socket is never briefly open to
  others.
- A relative `SPOT_TARGET` is made absolute even when the caller's folder is unknown.

## 0.8.3 — 2026-11-27

- The daemon's log and the launch history (`frecency.json`) are readable by their owner only (mode
  0600), including ones left by an earlier release.

## 0.8.2 — 2026-11-26

- **Security: the daemon socket is checked.** The client and the daemon verify that the other end is
  your own user, and the fallback socket used without `XDG_RUNTIME_DIR` moved into a private folder
  (`/tmp/spot-<uid>/`) whose ownership is checked.

## 0.8.1 — 2026-11-26

- **A dialog that does not open says so.** A broken or missing config used to fail silently from a
  keybind; when stderr is not a terminal spot now also sends a notification (`notify` in
  `daemon.toml`, `SPOT_NO_NOTIFY`).
- Without a display spot exits with status 3 and says why, not with 1 (which means cancelled).

## 0.8.0 — 2026-11-26

- **Controls that apply at once.** `state` and `on_change` on a new `switch` (look: switch, button
  or checkbox), a new `segmented` control and `scale`; a new `button` and `separator`; and
  `switches = true` on lists. The dialog stays open, states are read again after each change, and a
  failed change is not shown as done.

## 0.7.4 — 2026-11-26

- **Open a file or URL with the picked app.** `SPOT_TARGET=<path or URL> spot launcher` fills the
  entry's `%f %F %u %U`.
- The app index reads `MimeType=`.

## 0.7.3 — 2026-11-26

- Releases are built in an Arch Linux container (`.jenkins/Dockerfile`): the bindings 0.7.2 moved to need
  GLib 2.88 or newer. The release binary needs the same. 0.7.2 was tagged but never released.

## 0.7.2 — 2026-11-26

- **The daemon no longer leaks memory.** Closed dialogs and their list rows are freed.
- The source is split into packages under `internal/` (no change in behaviour).

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
