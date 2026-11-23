# The daemon

Since 0.4.0 dialogs are shown by a background process, `spot daemon`, that owns
one layer-shell window. `spot <config>` is a thin client: it hands the config
path, its environment and any stdin to the daemon, waits for the answer, prints
it and exits with the usual [exit status](toml.md#running-it). Scripts do not
change.

## Contents

- [Why a daemon](#why-a-daemon)
- [Running it](#running-it)
  - [On demand (default)](#on-demand-default)
  - [systemd user service](#systemd-user-service)
  - [Compositor autostart](#compositor-autostart)
  - [Without the daemon](#without-the-daemon)
- [`daemon.toml`](#daemontoml)
- [Controlling it](#controlling-it)
- [How dialogs share the window](#how-dialogs-share-the-window)
- [Environment](#environment)
- [Files](#files)
- [Troubleshooting](#troubleshooting)

## Why a daemon

Moving from one dialog to the next reuses the window. A menu that opened a
sub-menu used to destroy one surface and create another, and the fade started
over each time; now the content is swapped in place. The daemon holds no state
of its own: the TOML and CSS are read again on every request, so edits show on
the next open.

## Running it

### On demand (default)

Nothing to set up. The first `spot` call starts `spot daemon` in the background
(that call is a moment slower, once) and later calls reuse it. Two calls that
race to start one end up with a single daemon. Its output goes to
`~/.local/state/spot/daemon.log` (see [`log`](#daemontoml)).

### systemd user service

Starting the daemon at login makes even the first dialog instant, and its
output goes to the journal. The AUR packages install the unit as
`/usr/lib/systemd/user/spot.service`; from a source checkout copy
[`packaging/systemd/spot.service`](../packaging/systemd/spot.service) to
`~/.config/systemd/user/` (and fix the `ExecStart` path if `spot` is not in
`/usr/bin`). Then:

```sh
systemctl --user daemon-reload          # only after copying the file by hand
systemctl --user enable --now spot.service
```

The unit is tied to `graphical-session.target`: it starts once the session is
up and stops with it. That needs a session that reaches the target with
`WAYLAND_DISPLAY` imported into the user manager, which **uwsm** does for
Hyprland, Sway and others. Without such a session manager either import the
variables yourself from the compositor's autostart:

```sh
# Hyprland: hyprland.conf
exec-once = systemctl --user import-environment WAYLAND_DISPLAY XDG_CURRENT_DESKTOP && systemctl --user start spot
```

or use [compositor autostart](#compositor-autostart) instead.

```sh
systemctl --user status spot      # is it running, which build
journalctl --user -u spot -f      # its log
systemctl --user restart spot     # after editing daemon.toml
```

Notes:

- `Restart=on-failure`: a crash brings it back; `spot --quit` stops it for good
  (until the next `spot` call starts one on demand, outside systemd).
- **Upgrades.** After a package upgrade the first `spot` call sees a daemon
  from the old build, stops it and starts the new one. That one runs outside
  the unit, logging to the [log file](#daemontoml). `systemctl --user restart
  spot` after upgrading keeps it under systemd.

### Compositor autostart

Without systemd, start it from the compositor:

```ini
# Hyprland
exec-once = spot daemon
# Hyprland under uwsm, so it gets its own scope
exec-once = uwsm-app -- spot daemon
```

```ini
# Sway
exec spot daemon
```

`spot daemon` exits at once (status 0) if one is already running on the same
socket, so a second start is harmless.

### Without the daemon

`--oneshot` or `SPOT_NO_DAEMON=1` shows the dialog in a window of its own, the
pre-0.4 way. spot also falls back to this when no daemon can be reached or
started (for example with no `WAYLAND_DISPLAY`). Chaining and `--sub` then do
nothing.

## `daemon.toml`

Optional settings, in `${XDG_CONFIG_HOME:-~/.config}/spot/daemon.toml`. It is
not a dialog: `spot daemon` is the subcommand, so the name never clashes with a
dialog config looked up by name. A commented copy with every key is
[`examples/daemon.toml`](../examples/daemon.toml).

```toml
# ~/.config/spot/daemon.toml
socket = "$XDG_RUNTIME_DIR/spot.sock"
log    = "~/.local/state/spot/daemon.log"
grace  = 120
chain  = 1500
```

| Key | Default | Description |
|-----|---------|-------------|
| `socket` | `$XDG_RUNTIME_DIR/spot.sock` | Unix socket the daemon listens on and clients connect to. `SPOT_SOCKET` overrides it. |
| `log` | `$XDG_STATE_HOME/spot/daemon.log` | Output of a daemon a client starts on demand. Truncated at each start. A daemon run by systemd logs to the journal and does not use it. |
| `grace` | `120` | Milliseconds the window stays mapped, empty, after a dialog closes, waiting for the next one. `0` releases it at once. |
| `chain` | `1500` | Milliseconds an answered `--chain` dialog (or one left with Back) stays on screen while the next dialog starts. |

- Paths expand `$VARS` and a leading `~/`; a relative path is relative to the
  file's directory.
- `grace` and `chain` take 0-60000.
- **Unknown keys are errors**, so a typo is reported instead of silently
  ignored.
- The daemon reads the file **once, when it starts**: restart it after an edit
  (`systemctl --user restart spot`, or `spot --quit`). Clients read it on every
  call, for `socket` and `log`.
- A **broken file** is reported on stderr (the journal, for the service) and
  ignored: spot runs with the defaults rather than refusing to show dialogs.
- `--oneshot` dialogs do not use it.

When to change the timing:

- Scripts that take a while between menus (a slow `nmcli`, say) make the window
  vanish and come back: raise `grace`, or call the first dialog with `--chain`.
- A `--chain` dialog that is followed by nothing stays on screen for `chain` ms;
  lower it, or end such flows with `--last`.

## Controlling it

| Command | |
|---------|-|
| `spot daemon` | Run the daemon in the foreground (what the service and on-demand start do) |
| `spot --status` | `spot daemon: running (0.4.0), client 0.4.0`, or `not running` (status 1) |
| `spot --dismiss` | Cancel every dialog shown, covered or waiting (their callers exit 1), and release the window |
| `spot --quit` | Stop the daemon once the current dialogs are done |
| `SIGTERM` / `SIGINT` | Cancel everything and stop at once (`systemctl --user stop spot`) |

A daemon from a different build is replaced automatically on the next call, so
version mismatches after an upgrade need no action.

## How dialogs share the window

- **One at a time.** A request that arrives while a dialog is up waits its
  turn, in order.
- **`--sub`** opens on top of the dialog that is showing; that one comes back,
  state intact, when the sub-dialog closes. Sub-dialogs nest.
- **Grace.** When a dialog closes and nothing is waiting, the window stays
  mapped with the dialog hidden for [`grace`](#daemontoml) ms. A request in
  that time takes over the same surface; otherwise the surface is released.
- **`--chain`** keeps an *answered* dialog visible for up to
  [`chain`](#daemontoml) ms, so a menu hands over without any gap. A cancelled
  one never holds the window. Back holds it the same way, since the dialog it
  returns to is being started.
- **`--last`** releases the window at once.
- **Cancelling.** Esc or a click outside closes the top dialog. If a client is
  killed (Ctrl-C, script ends), its dialog goes with it.
- **`submit.exec`** runs without blocking the daemon, so a command may itself
  call `spot` (typically with `--sub`).

## Environment

Each request carries the caller's environment and working directory.
`SPOT_*` overrides and `submit.exec` / `spawn` commands use **the caller's**,
not the daemon's, so a service with a minimal environment still runs commands
the way the calling script would.

| Variable | Read by | |
|----------|---------|-|
| `SPOT_SOCKET` | client, daemon | Socket path; wins over `daemon.toml`. Run a second, separate daemon with it. |
| `SPOT_NO_DAEMON` | client | Any value: same as `--oneshot` |
| `WAYLAND_DISPLAY` | client, daemon | Without it the client does not try the daemon; the daemon needs it to draw |
| `XDG_CONFIG_HOME` | client, daemon | Where `spot/daemon.toml` is looked for |
| `XDG_RUNTIME_DIR` | client, daemon | Default socket directory (else `/tmp/spot-<uid>.sock`) |
| `XDG_STATE_HOME` | client | Default log directory |

## Files

| Path | |
|------|-|
| `~/.config/spot/daemon.toml` | Optional settings (above) |
| `$XDG_RUNTIME_DIR/spot.sock` | The socket (mode 0600) |
| `$XDG_RUNTIME_DIR/spot.sock.lock` | Held while a daemon runs; makes concurrent starts end with one |
| `~/.local/state/spot/daemon.log` | Output of an on-demand daemon |
| `/usr/lib/systemd/user/spot.service` | The user unit (AUR packages) |

## Troubleshooting

- **Dialogs open in their own window, no chaining.** The daemon could not be
  reached or started. Check `spot --status`, then the log
  (`journalctl --user -u spot` or `~/.local/state/spot/daemon.log`). A common
  cause is a service started before `WAYLAND_DISPLAY` was imported: see
  [systemd user service](#systemd-user-service).
- **The service is active but `spot` starts another daemon.** The two disagree
  on the socket: the service's environment has a different `XDG_RUNTIME_DIR`
  or `SPOT_SOCKET`. Set `socket` in `daemon.toml`, which both read.
- **Edits to `daemon.toml` have no effect.** Restart the daemon.
- **`spot: …daemon.toml: …(using daemon defaults)`** on every call: fix or
  remove the file.
