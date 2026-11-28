# spot as a framework: menus and mod levels

> **Draft. Nothing here is built.** What was decided on 2026-11-28 is marked **decided**; the rest is open.
> It builds on [`modding.md`](modding.md) and covers [0.10, menus](https://github.com/KopyTKG/spot/issues/25).

## The goal

A repository holding a whole desktop setup (menus, dialogs, bar modules, mods) that someone else can
clone, read and make their own. As few shell scripts as is sensible.

**For now:** two users, both on Arch with Hyprland. The repositories are public, so the design assumes
strangers will run them, but nothing is built for other systems until somebody needs it.

## What has to be said first

1. **A config is a program.** `exec` in a dialog runs as you, with no sandbox and no approval. So will a
   menu entry. A cloned setup's configs can do anything a shell script can.
2. **An `install.sh` is a program** too. Whoever runs one from a repository has trusted it completely,
   before spot is involved.
3. So neither mod levels nor fingerprints make a cloned setup safe. What they can do is: keep one mod
   within what it says, and stop what **changed** after you looked at it.
4. **One unsandboxed mod ends every promise.** It runs as you. Even with approvals only root can write,
   it can replace the daemon that reads them, or imitate its dialogs.

Approval must never claim more than this.

## What a script needs

From the 66 scripts of `hypr-setup`, by their headers and the commands they call:

| Group | Scripts | Answer here |
|---|---|---|
| Interactive terminal tools (installers, pickers) | 18 | stay scripts |
| Hyprland actions | 17 | about 10: [Lua in the Hyprland config](#not-spots-job); the rest [built-in actions](#built-in-actions) |
| Services, devices, indicators | 11 | built-in actions |
| Data for a dialog or bar | 12 | mods, as today |
| Menus and glue | 8 | [menus](#menus-010), in part |

## Menus (0.10)

`arch-menu` is 507 lines: about twenty menus, a `case` for each, and window handling between them. As
TOML:

```toml
include = ["menu/*.toml"]            # decided: includes; relative to this file, below its folder only

[page.main]
title = "Menu"

[[page.main.entry]]
label = "Capture"
page  = "capture"                    # goes there; Back returns

[[page.main.entry]]
label    = "Update"
terminal = "fish -c update"          # opens in your terminal

[page.capture]
title = "Capture"

[[page.capture.entry]]
label = "Region"
exec  = "hyprshot -m region"

[page.remote]
from = "ssh.hosts"                   # entries from a mod
call = "ssh.connect"                 # picked entry's key goes to the mod
```

- One window; Back and Esc behave as they do between chained dialogs now.
- An entry does exactly one of: `page`, `exec`, `terminal`, `open` (a URL or file), `call`.
- Typing searches every static entry on every page, and shows the path ("Capture › Region").
- A page from a mod is fetched when it is opened, and is searched only once fetched.
- **Data never becomes a command:** a key from a mod goes to `call`, or to `exec` only as
  `$SPOT_VALUE_<ID>`, as in dialogs today. A page from a mod cannot supply `exec`.

**Proof:** `arch-menu` replaced, feeling the same.

## Not spot's job

Hyprland's own Lua config can bind a function to a key, read windows and monitors, and change settings
(`hl.bind(keys, function)`, `hl.get_active_window`, `hl.config`, `hl.dispatch`; from the API definitions
of 0.57.2). Toggles such as gaps, window transparency, window pop and monitor scale need nothing from
spot. A menu entry could call such a function through `hyprctl eval`; **not tried**.

## Approvals only root can write

**Decided: yes.** Records move from your home to `/var/lib/spot/approvals/<uid>/`.

- A small root-owned helper writes them, and does nothing else. It is run with `sudo -k`, so **every
  approval asks the password**.
- Reading them needs no root.
- **It stops** a program running as you from quietly adding or changing an approval.
- **It does not stop** that program from replacing or imitating the daemon, running its own `spot`, or
  asking you for your password itself.
- Existing approvals stop counting once; each mod is approved again.
- Open detail: the helper should compute the fingerprint from the files itself, so that what is recorded
  is what is there, and not what an unprivileged program claims.

## Fingerprints for configs and scripts

**Decided:** configs get fingerprints too, **CSS included** (new styling should not load unannounced),
and so do **the scripts they call**. No folder is exempt, your own included.

`spot` cannot work out reliably which scripts a command runs: a script can call others however it
likes. So whole folders are covered, named by the setup (**decided:** named folders, not the whole
repository):

```toml
# setup.toml, at the root of the setup
covers = ["spot-config", "bin", "lib"]
```

- Every file in them has its SHA-256 recorded, and so does `setup.toml` itself.
- A change in any of them stops the configs from opening until approved again. A notification says so;
  `spot configs diff` shows what changed; `spot configs allow` approves.
- Approval shows the covered folders and every command the configs run. A covered folder that is
  **removed** from the list is said first, and loudest.
- A command in a config that points outside the covered folders is listed as **not covered**. System
  programs are the package manager's business.
- **It only guards what `spot` runs.** A keybinding, a bar or a shell that starts a script from `bin/`
  runs it whether it changed or not.

## The manager

**Decided: build it.** A window built into `spot` that shows every mod and setup, what is known about
each, and takes approvals. Opened with `spot manage`.

| It shows | |
|---|---|
| Each mod | what it is for, its level, every permission with its reason, scanner flags, whether it runs, recent errors and the audit log |
| Each setup | covered folders, every command its configs run, what is not covered, what changed |
| Actions | read the files, see the diff, approve, revoke |

- **First setup** is the same window, listing what waits for review after something was cloned. It
  approves nothing by itself.
- **The password is the gate.** Other programs on a desktop can fake clicks and typing, so a button
  alone means nothing. The root-owned approvals are what make approving here no weaker than at a
  terminal.
- **Built in,** not a TOML dialog, and **your CSS does not apply to it**: a setup must not be able to
  restyle it, hide a warning or ship a lookalike as a config.
- One thing at a time, no "approve all". Dangerous permissions are still typed by name.
- **It never asks for a password unprompted.** You are notified and open it yourself.
- Text from mods and configs is shown with control and direction characters made visible, as at the
  terminal today.
- The same checks as the terminal commands, which stay for use without a desktop.
- Needs a view for long text and diffs, which `spot` does not have.
- Something already running as you can still draw a window that looks like it.

## Mod levels

| Level | Declared as | The mod can | Promise |
|---|---|---|---|
| **Sandboxed** | today's permissions | its listed files and programs | enforced by the kernel |
| **Built-in actions** | new permission kinds | ask spot to do narrow things for it | each action is checked by spot |
| **Unsandboxed** | `level = "unsandboxed"` | anything you can | **none** |

Parsers are always sandboxed. **No level runs a mod as root.**

### Built-in actions

The mod stays in its sandbox and sends a request; **the daemon** checks it against the approval and does
it. This is a new direction on the channel (host asks daemon) and its most sensitive part.

```toml
[permissions.hyprland]
read   = "list windows and monitors"
window = "focus, close and pop windows"

[permissions.service]
"hypridle.service" = "turn the idle lock on and off"
```

| Kind | Actions | Trap, and the rule |
|---|---|---|
| `hyprland.read` | monitors, windows, workspace, binds | Window titles are private data: said at approval |
| `hyprland.window`, `.monitor`, `.look`, `.input` | a fixed list each, with typed values | Raw `hyprctl` or `eval` can start any program: **never offered**. spot builds the command; a value is never pasted into Lua |
| `service` | start, stop, state of listed user units | A unit runs what its file says: only units a mod cannot write |
| `process` | is it running, signal it, by listed name | Any process of that name |
| `notify` | a notification | Always prefixed with the mod's name |
| `clipboard.write` | set it | Can plant a command you later paste |
| `clipboard.read` | read it | Passwords pass through it: **dangerous**, typed |
| `open` | a URL in the browser | A URL can carry data out: **dangerous**, listed hosts only |
| `terminal` | a listed command in your terminal | That program runs outside the sandbox: **dangerous**, typed in full |
| `keys` | send a paste to the focused window | Pasting into a terminal runs commands: **not a built-in**, unsandboxed only |

### Root

```toml
[[permissions.root]]
command = ["ufw", "delete", "allow", "{port}/{proto}"]
port    = "port"                     # typed: port, or one of a list
proto   = ["tcp", "udp"]
reason  = "delete a rule"
```

- Only commands listed in this form, with typed holes. No shell, no free text.
- **Every** run shows the exact command in a dialog spot draws; the mod cannot word it.
- The password is asked **every time** (`sudo -k`). A cached `sudo` would run the command unseen, which
  `arch-root` allows today.
- The mod process never has root and never sees the password.
- A program that already runs as you can imitate any dialog. This does not protect against that.

### Unsandboxed (alpha)

**Decided: try it as an alpha.** It stays if using it earns it, and is deleted if not. It may be changed
or removed in any release, and says so at approval.

- Declared in the mod **and** enabled by name in your own `mods.toml`:
  `[mod.wallpaper] unsandboxed = true`. Either alone does nothing.
- Approval, at a terminal, says: **"This mod can do anything you can. It can change what you approved
  for other mods. spot cannot limit or record it."** Then you type the mod's name and `unsandboxed`,
  and your password.
- `spot mods` marks it every time, and while any is approved, says the others' approvals can no longer
  be relied on.
- It is not safer than a shell script. It gains one structure, dialogs wired to data, and the notice
  when it changes.
- **Decided:** it needs Landlock ABI 11 like every other mod, although it uses none of it. Otherwise it
  would become the way to run mods on an old kernel, with no protection at all. Both users have a new
  kernel, so this costs nothing now.

## A setup someone clones

- Cloning, linking or `spot` starting **never** approves anything. Approval is per machine, by hand.
- A repository's `mods.toml` can list mods, but the `unsandboxed` line is the one to look at, and spot
  names it at approval.
- After `git pull`, a changed mod does not run until approved again, and `spot mods diff` shows why.
- Its configs and covered scripts are stopped the same way until you approve them; `install.sh`, and
  whatever runs without `spot`, are not.

## Security rules

To hold before a level ships, each with a test.

| | Rule |
|---|---|
| F1 | The daemon performs a built-in action only for a mod whose approval lists it, checked at each request |
| F2 | Values of an action are typed and checked by the daemon, never by the mod alone, and never joined into a command string or Lua |
| F3 | There is no action that runs an arbitrary program, evaluates text, or sends keys |
| F4 | A new or widened action voids the approval |
| F5 | A root command runs only as listed, after showing it and asking the password that time |
| F6 | Unsandboxed needs both the mod's declaration and your own setting, plus typed approval; there is no flag to skip it |
| F7 | Parsers can neither use actions nor be unsandboxed |
| F8 | A page or entry from a mod cannot contain a command |
| F9 | Every action and root run is in the mod's audit log |
| F10 | An approval record that is not root's, or that others can write, counts for nothing |
| F11 | The helper writes only approval records of the user who ran it, and asks the password each time |
| F12 | Nothing inside a repository can switch a check off: not for a mod, not for a setup |
| F13 | A covered file that changed is not run by spot: checked when a config is opened and again before a command |
| F14 | The manager takes no style, text of its own interface or action from a config or mod |

## Order

1. **0.10 menus.** Safe, visible, removes the largest script.
2. **Root-owned approvals,** then **fingerprints**, which rest on them, then **the manager**. A manager
   that only shows things can come earlier, to try its layout.
3. **Built-in actions**, one kind at a time, each proven by replacing real scripts: `service` and
   `process` first (idle lock, waybar).
4. **Root commands**, proven by moving the firewall's changes into its mod.
5. **Unsandboxed, alpha**, only for what is left. It may be very little: wallpaper, key list, update
   check. A month after it is released, keep or delete it.

## Still to work out

None of these blocks menus.

- What the root-owned helper reads and shows by itself, so that it need not trust `spot`.
- How a covered file is checked right before a command without making menus feel slow.
- Whether `hyprctl eval` can call a function of the Hyprland config from a menu entry.
