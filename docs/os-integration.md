# Tying spot to your system

spot draws a window and hands you an answer. Everything that touches the system (listing
windows, copying to the clipboard, rebooting, asking for a root password) is a **script that
calls spot**. This page shows the patterns, using the runnable scripts in
[`../examples/os/`](../examples/os/).

## Contents

- [The shape of a spot script](#the-shape-of-a-spot-script)
- [Pattern: pick from a list a command produced](#pattern-pick-from-a-list-a-command-produced)
- [Pattern: tiles, then a `case`](#pattern-tiles-then-a-case)
- [Pattern: a table with a header](#pattern-a-table-with-a-header)
- [Pattern: folding groups](#pattern-folding-groups)
- [Pattern: asking for a password](#pattern-asking-for-a-password)
- [Wiring it to your compositor](#wiring-it-to-your-compositor)
- [Menus that lead to menus](#menus-that-lead-to-menus)
- [Writing scripts that behave](#writing-scripts-that-behave)
- [Testing a spot script without a screen](#testing-a-spot-script-without-a-screen)

## The shape of a spot script

```bash
#!/bin/bash
here=$(dirname "$(readlink -f "$0")")                 # the config sits next to the script

choice=$(some-command | spot "$here/pick.toml") || exit 0   # rows in, the chosen row out
[[ -n $choice ]] || exit 0

do-something-with "$choice"
```

Three ways data moves between a script and spot:

| Direction | How |
|-----------|-----|
| **Into spot** | a list reads one option per line from **stdin** (`stdin = true`); `SPOT_TITLE`, `SPOT_FOOTER`, `SPOT_PLACEHOLDER`, `SPOT_DEFAULT`, `SPOT_QUERY` set text per call, so one config serves many scripts |
| **Out of spot** | the answer is printed to **stdout** (`[submit] stdout = "{id}"`, or `format = "json"` for every field), and the **exit status** says how it ended |
| **Straight to a command** | `[submit] exec = "… {field} …"` runs a command with the fields substituted (shell-quoted); use it for simple forms, a script when you need logic |

The exit status is the contract: `0` an answer was given, `1` it was cancelled (Esc, a click
outside) and **nothing is printed**, `3` the config is broken. So `|| exit 0` after `spot` means
"if the user backed out, do nothing".

## Pattern: pick from a list a command produced

[`clipboard.sh`](../examples/os/clipboard.sh) and [`windows.sh`](../examples/os/windows.sh): a
command prints rows, spot shows them, the script finds out *which* one was chosen.

```bash
choice=$(cut -f2- <<<"$history" | spot "$here/clipboard.toml") || exit 0
entry=$(PICK=$choice awk -F'\t' '{ p=$0; sub(/^[^\t]*\t/,"",p); if (p==ENVIRON["PICK"]) {print; exit} }' <<<"$history")
cliphist decode <<<"$entry" | wl-copy
```

spot shows only the label (`cut` drops the ID column) and prints the label back, so the script looks
the full record up again. Pass the label to `awk` through the *environment* (`ENVIRON`), not `-v`,
which would interpret backslashes in it. If two rows can share a label (two windows with the same
title) the first match wins; if that matters, put something unique in the label.

## Pattern: tiles, then a `case`

[`power-menu.sh`](../examples/os/power-menu.sh): the config is a row of tiles whose `value`s are
words; the script maps words to commands.

```bash
choice=$(spot "$here/power-menu.toml") || exit 0
case $choice in
  lock)     loginctl lock-session ;;
  reboot)   ask "Restart now?" && systemctl reboot ;;
esac
```

Keeping the *words* in the TOML and the *commands* in the script means a theme or layout change
never touches the logic, and you can see every command the menu can run in one place. The
destructive ones ask first, with a second dialog (`confirm.toml` and a `SPOT_TITLE` that carries the
question).

## Pattern: a table with a header

[`sockets.sh`](../examples/os/sockets.sh): a list is monospace, so fixed-width columns line up. The
header is the list's **`label`**, which spot draws above the rows, and it must equal what the script
prints for the rows' columns. The script prints it (`sockets.sh header`) so the two stay in sync, and a
test in this repository checks that the TOML label matches. Truncate cells to the column width
(with a `~`) so one long value cannot push the rest out of line. Sorting is the script's job
(listeners first, then by port); spot only filters.

## Pattern: folding groups

[`emoji.sh`](../examples/os/emoji.sh): when a flat list would be thousands of rows, group it. Each
line is `GROUP<TAB>ROW`; with `collapsible = true` the groups start folded and typing searches inside
all of them (the group name counts, so `animals cat` works). Enter on a row returns the row without its
group. Also good for help screens and keybind lists.

## Pattern: asking for a password

[`askpass.sh`](../examples/os/askpass.sh) and [`root.sh`](../examples/os/root.sh): `sudo` and `ssh`
accept an *askpass helper*, a program they run when they need a password, with the prompt as `$1`,
and read the password from its stdout.

```bash
SUDO_ASKPASS=/path/askpass.sh sudo -A systemctl restart nginx
SSH_ASKPASS=/path/askpass.sh SSH_ASKPASS_REQUIRE=force ssh host
```

The script is one `spot` call with a `password` item. `root.sh <reason> <command…>` sets
`ASKPASS_REASON`, which the dialog shows in its footer (`To: restart nginx`), so the person typing
knows what they are authorising. This replaces `pkexec`, whose polkit window comes from another
program and looks like it. The password goes to stdout and nowhere else; an empty answer or Esc makes
the helper fail, which makes `sudo` fail.

## Wiring it to your compositor

spot has no keybindings of its own; the compositor runs the script.

```lua
-- Hyprland, Lua config
hl.bind("SUPER + ESCAPE",    hl.dsp.exec_cmd("~/bin/power-menu.sh"))
hl.bind("SUPER + CTRL + V",  hl.dsp.exec_cmd("~/bin/clipboard.sh"))
hl.exec_cmd("spot daemon")                        -- once at login: the first press is instant
```

```ini
# Hyprland, hyprlang
bind = SUPER, Escape, exec, ~/bin/power-menu.sh
exec-once = spot daemon
```

```
# sway
bindsym $mod+Escape exec ~/bin/power-menu.sh
exec spot daemon
```

- **Put `toggle = true` in the dialog's config** so the key closes what it opened and a held key does
  not queue a pile of windows.
- **Close from anywhere** with `spot --dismiss` (a "close the top thing" key can try it first and fall
  back to closing the window).
- **systemd users** can run the daemon as a service instead: see [`daemon.md`](daemon.md).
- A keybind's environment is not your terminal's. Give scripts absolute paths or put them on the
  `PATH` the compositor exports.

## Menus that lead to menus

Whether a dialog offers **Back** is decided by whoever opens it, through the environment, so a script
needs no changes to be both a top-level key and a sub-menu:

```bash
SPOT_BACK="main-menu.sh" ./display.sh      # display.sh shows Back, which returns to the main menu
./display.sh                               # opened by a key: no Back
```

`--chain` keeps the window mapped while the next dialog is being prepared, so a menu hands over without
a flash. [`../examples/back.sh`](../examples/back.sh) is a runnable walk-through.

## Writing scripts that behave

- **Quote everything that comes from a person.** Never splice typed text into a command string. Use
  spot's `{field}` placeholders in `exec` / `activate` (shell-quoted for you), or take the value into
  a variable and use `"$var"`.
- **Check your tools** (`command -v cliphist`) and say what is missing with `notify-send`: a
  keybound script has no terminal to print to.
- **Esc is not an error.** Exit quietly (`|| exit 0`).
- **spot reads the caller's environment**, not the daemon's: `$HOME`, `$PATH`, `$XDG_RUNTIME_DIR` in a
  `submit.exec` are the script's.
- **Slow commands belong in a source**, not in the script before the dialog opens, if you want the
  window up at once: see [`launcher.md`](launcher.md#sources).
- **Keep logic in the script, words in the TOML.** The config describes what is shown, the script what
  happens.

## Testing a spot script without a screen

A script's logic can be tested by replacing `spot` and the system commands with stand-ins on `PATH`:

```bash
# fakes/spot: answer with $PICK, remember what was shown
cat > fakes/spot <<'STUB'
#!/bin/bash
cat > "$TESTDIR/shown" ; printf '%s\n' "$PICK"
STUB
printf '#!/bin/bash\necho "$0 $*" >> "$TESTDIR/ran"\n' > fakes/systemctl
chmod +x fakes/*

PATH=$PWD/fakes:$PATH TESTDIR=/tmp/t PICK=reboot ./power-menu.sh
cat /tmp/t/ran                                    # systemctl reboot  (after the confirm answered "reboot"…)
```

Give each fake the answer your case needs (`PICK=` empty simulates Esc: make the stub `exit 1`), and
assert on what was run. Only read stdin in the stub for configs that take it, or it will wait forever.
Where the script reads the real system (`ss`, `hyprctl clients`), run it for real and look at the rows.
