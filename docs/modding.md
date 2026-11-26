# Modding: daemon mods (draft)

> **Draft for review. Nothing described here exists yet.** It proposes the mod API for 0.9
> ([#16](https://github.com/KopyTKG/spot/issues/16)); names and behaviour are all open to change.
> Questions to answer are [at the end](#open-questions).

A **daemon mod** is a Lua program the spot daemon looks after. It loads and parses data once, keeps
it, and hands it to anything that asks: dialogs, a bar, scripts. It replaces the shell script that
is run again for every consumer.

Mods do **not** run inside the daemon. Each provider runs in its own **mod host** process, so a mod
that hangs or eats memory can be killed without taking your launcher and menus with it.

```
 ss, ufw,    ┌─ mod host ───────┐              ┌─ spot daemon ─┐
 files  ────►│ mod "sockets"    │◄─ messages ─►│               │──► dialogs  (from = "sockets.table")
             └──────────────────┘              │  keeps what   │──► bars     (spot watch sockets.count)
             ┌─ mod host ───────┐              │  mods publish │──► scripts  (spot get / spot call)
             │ mod "idle"       │◄─ messages ─►│               │◄── requests (spot call idle.set false)
             └──────────────────┘              └───────────────┘
```

**UI mods** (behaviour inside one dialog) are a separate layer. They come later, with no release set,
and are not covered here. The work for 0.9 is tracked in the [milestone](https://github.com/KopyTKG/spot/milestone/1).

## Types of mod

| Type | Does | May | Runs |
|------|------|-----|------|
| `parser` | turns text into data | compute only: no commands, files, timers or publishing | in a locked Lua state of its own, inside the mod host that uses it |
| `provider` | gets data (usually with a parser), publishes it, answers requests | what its [permissions](#permissions) grant: reading and writing files, running commands | in its own mod host, as long as the daemon runs |

A mod says what it is in `mod.toml` next to its `init.lua`, so spot knows what it may do before
running any of it. Without the file a mod is a provider; a parser must have one, with
[its contract](#the-parser-contract).

Splitting them means the parsing of `ss`, `ufw status` or `~/.ssh/config` is written once, shared
by every tool that needs it, and testable without running anything.

## A complete example

A parser, `~/.config/spot/mods/ss/`. Its `mod.toml` says what goes in and what comes out:

```toml
type        = "parser"
api         = 1                          # the version of this contract
version     = "1.0.0"
description = "Sockets from ss"
input       = "output of `ss -H -tuna`"

[returns]                                # the fields of each record
label   = "string"
key     = "string"
comment = "string?"
```

Its `init.lua` returns exactly one function:

```lua
return function(input, options)
  local rows = {}
  for line in input:gmatch("[^\n]+") do
    local proto, state, _, _, addr, peer = line:match("(%S+)%s+(%S+)%s+(%S+)%s+(%S+)%s+(%S+)%s+(%S+)")
    if proto then
      rows[#rows + 1] = { label = ("%-4s %-9s %s  %s"):format(proto, state, addr, peer), key = addr }
    end
  end
  return rows
end
```

A provider that uses it, `~/.config/spot/mods/sockets/init.lua`:

```lua
local parse = spot.use("ss")

local function sockets()
  local r = spot.run({ "ss", "-H", "-tuna" })
  if r.status ~= 0 then error(r.stderr) end
  return parse(r.stdout)
end

spot.value("table", { ttl = 2000 }, sockets)

spot.value("count", { every = 5000 }, function()
  local rows = sockets()
  return { text = tostring(#rows), tooltip = #rows .. " sockets" }
end)
```

It says what it needs in its `mod.toml`:

```toml
uses = ["ss"]

[permissions.run]
ss = "list the open sockets"
```

Enable the provider in `~/.config/spot/daemon.toml`, and approve what it asks for. The parser it uses
comes with it:

```toml
mods = ["sockets"]
```

```sh
spot mods allow sockets
```

Use it:

```toml
# sockets.toml
[[items]]
type  = "list"
id    = "socket"
from  = "sockets.table"
match = "smart"
```

```jsonc
// waybar
"custom/sockets": { "exec": "spot watch --json sockets.count", "return-type": "json" }
```

## Parsers

A parser written by one person has to work for anyone who downloads it, so every parser has the same
shape and cannot do anything but parse.

### The parser contract

**One function.** `init.lua` returns it and nothing else. A parser is that one file; what it shares
with others it gets from another parser through `spot.use`, declared in `uses`:

```lua
return function(input, options) ... return output end
```

| | |
|---|---|
| `input` | a string: the text to parse |
| `options` | a table of plain data, empty when none are given |
| `output` | a **list of records**, whose fields are declared in `[returns]` |
| Failure | `error("what was wrong")`, the same for every parser |

**Always a list of records.** A record is a table with named fields. There is no other kind of
output, so every parser is used the same way: loop over what it returns.

| Case | The parser returns |
|------|--------------------|
| Many things (sockets, firewall rules) | one record each |
| One thing (a battery level) | a list of one: `{ { percent = 87 } }` |
| Nothing found | an empty list: `{}` |
| The input could not be parsed | `error()`, so that it never looks like "nothing found" |

A table that is not a list, a list holding anything but records, or a bare string or number is an
error. A fixed outer type also settles what Lua leaves open: an empty result is an empty list, never
an empty map.

**Plain data.** Field values are strings, numbers, booleans and tables of those. Functions, threads
and tables that contain themselves or nest more than 32 deep are refused.

**Declared fields.** `mod.toml` of a parser must have `api`, `version`, `description`, `input` and
`[returns]`. Each line of `[returns]` is a field of a record and its type: `"string"`, `"number"`,
`"boolean"`, `"list"` or `"map"`, with `?` when it may be missing.

Only what is declared is checked, as deep as the author chooses to promise: undeclared fields, and
whatever is inside a `list` or `map`, pass through. A record that does not match is an error, so a
user of the parser can rely on the declaration without reading the code. A change that breaks it
needs a new major `version`.

### Calling it

Every parser is called the same way, by the name of its folder:

```lua
local parse = spot.use("ss")                 -- in a mod
local rows  = parse(text, { listening = true })
```

```sh
ss -H -tuna | spot parse ss                  # in a shell, prints a JSON array
```

```toml
# in a dialog: run a command, show what the parser makes of it
[[items]]
type  = "list"
id    = "socket"
run   = "ss -H -tuna"
parse = "ss"                                  # its records need a label
```

So a tool can start as a command and a parser, and become a provider only when it needs to keep
data, refresh it or answer requests. A parser named by a dialog or `spot parse` needs no enabling:
it runs in a mod host started for that one call.

### Locked down

- **Its own Lua state.** Text and options are copied in and plain data is copied out. No function,
  table or `spot` is ever shared with the mod that calls it. This is the barrier: loaded into the
  caller's state instead, a test parser wrote a file through the caller's `spot.write`, and even
  with its own table of globals it replaced the caller's `string.format`.
- **An allow-list of globals**, below. What is not on it is not there.
- **No way out:** no commands, files, network, environment, timers or publishing.
- **Same input, same output:** no clock and no `math.random`.
- Its `spot` has only `spot.use` (other parsers, in the same locked state), `spot.json` and
  `spot.log`.

### Checked on every call

Spot checks a parser's output as it is copied to the caller, always, so that no provider or dialog
sees data of the wrong shape: one function, a list, every item a record, every required field present
with its declared type, plain data, within the limits. One bad record fails the whole call and the
error says where:

```
parser ss: record 17, field "key": expected string, got nil
```

Dropping bad records silently would look like "there are fewer sockets".

### Checking while writing

```sh
spot mods check ss                          # the contract and the saved samples
ss -H -tuna | spot mods check ss -          # the same rules on live input
```

| Check | |
|-------|---|
| `init.lua` and `mod.toml` follow the contract | |
| Each `example/<name>.input` gives its `example/<name>.json` | several cases, not only the usual one |
| Empty input and junk input give `{}` or `error()` | never a crash or a wrong shape |
| The same input twice gives the same output | |
| Time and memory used, against the limits | a slow pattern shows up here, not in a menu |
| Fields returned but not declared; optional fields never seen | warnings: the declaration may be out of date |

It exits non-zero on failure, so a repository of shared parsers can run it in CI. It cannot say that
the parsing is **correct** for input it has no sample of: a parser can match the shape and still read
the wrong column.

## Files and enabling

| | |
|---|---|
| A mod | a folder `<name>/` with `init.lua` and `mod.toml` (optional for a provider). A provider can load other files of its folder with `spot.require` |
| Where | `$XDG_CONFIG_HOME/spot/mods/`, or `mods_dir` in `daemon.toml` |
| Name | `[a-z][a-z0-9_-]*`; it prefixes everything the mod publishes |
| Enabled | a provider: listed in `mods` in `daemon.toml` **and** approved with `spot mods allow`, otherwise it never runs. A parser: when something uses it |
| Settings | the `[mod.<name>]` table of `daemon.toml`, visible to the mod as `spot.config` |

## The API

This is what a **provider** gets. (A parser gets far less: see [locked down](#locked-down).)

A provider reaches your system only through one global, `spot`, and only as far as its
[permissions](#permissions) go. Lua's own `io` and `os` are not loaded:
`spot` offers what they would, in one way, with a time limit on every command and its normal memory
limit restored. Lua libraries that expect `io` or `os` therefore do not work. They may be added
later, which would break no mod; taking them away later would.

### The Lua environment

Lua 5.1. Parsers and providers start from the same **allow-list**; everything else is removed before
any mod code runs:

`assert` `error` `getmetatable` `ipairs` `next` `pairs` `pcall` `rawequal` `rawget` `rawset`
`select` `setmetatable` `tonumber` `tostring` `type` `unpack` `xpcall`, and the libraries `string`,
`table`, `math`, `coroutine`.

- A provider adds its `spot` to this. A parser adds a very small one and loses `math.random` and
  `math.randomseed`.
- `io`, `os`, `package` and `debug` are not loaded.
- The Lua library's base functions include some that read files, `dofile` and `loadfile`, even with
  `io` left out. An allow-list keeps them out, and also whatever a later version of the library adds.
- Code is loaded only by spot, and only from the mods folder: `spot.use` (parsers) and
  `spot.require` (a provider's own files).

### Publishing

| Call | Meaning |
|------|---------|
| `spot.value(name, options, fn)` | A value computed by `fn` **when somebody wants it**. `ttl` (ms): reuse the result that long. `every` (ms): recompute on this interval, but only while somebody is watching |
| `spot.publish(name, data)` | Set a value now, for mods that learn of changes themselves |
| `spot.refresh(name)` | Recompute a `value` now and tell watchers |

A value is a string, a number, a boolean, a table, or a list of rows. A **row** is
`{ label, key, state, comment, icon }`; only `label` is required. `state` (a boolean) makes it a
switch row. A value holds at most 10,000 rows (see [what was tested](#what-was-tested)).

Nothing is computed for a value nobody has asked for.

### Requests

```lua
spot.on("set", function(args)           -- spot call idle.set false
  local on = args[1] == "true"
  spot.run({ "systemctl", "--user", on and "start" or "stop", "hypridle" })
  spot.refresh("enabled")
  return on                             -- printed by spot call
end)
```

Arguments arrive as a list of strings. A Lua error becomes a failed call: non-zero exit status, the
message on stderr, and in a dialog the control goes back to what it showed.

### Helpers

| Call | Returns / does |
|------|----------------|
| `spot.run(argv, options)` | `{ status, stdout, stderr }`. `argv` is a list; **there is no shell**. Options: `timeout` (ms, default 5000), `stdin`, `env`, `dir` |
| `spot.pipe(argv1, argv2, …, options)` | The same for a [pipeline](#pipelines): each list is one command, connected without a shell |
| `spot.read(path)`, `spot.write(path, text)` | Whole files; `nil, message` on failure |
| `spot.lines(path)` | Iterate over a file line by line |
| `spot.list(path)` | The names in a folder |
| `spot.exists(path)` | `"file"`, `"dir"` or `nil` |
| `spot.every(ms, fn)`, `spot.after(ms, fn)` | Timers |
| `spot.json.encode(v)`, `spot.json.decode(s)` | JSON |
| `spot.env(name)`, `spot.time()` | An environment variable; seconds since 1970 |
| `spot.use(name)`, `spot.require(file)` | A parser's function; another file of this mod |
| `spot.log(message)` | To the daemon log, with the mod's name |
| `spot.watch_file(path, fn)` | `fn` when the file changes (**later**, not in the first version) |

Mods run in the **daemon's** environment, not that of whoever asks. `spot.env` reads only variables
the mod has permission for, and programs it starts get a small environment of their own
([S11](#the-security-contract)), not the daemon's.

### Pipelines

```lua
local r = spot.pipe({ "ss", "-H", "-tuna" }, { "grep", "LISTEN" }, { timeout = 2000 })
```

| | |
|---|---|
| `status` | that of the last command that failed, 0 if none did (a shell's `pipefail`) |
| `statuses` | each command's own, in order |
| `stdout` | of the last command |
| `stderr` | collected from every command |
| Options | `timeout` for the whole pipeline, `stdin` for the first command, `env` and `dir` for all |
| Permissions | **every** command must be allowed; if one is not, none is started |
| Cleanup | on a timeout, or when one cannot start, all of them are killed |

A non-zero status is not an error by itself: `grep` exits 1 when it finds nothing. The mod decides
what it means. There is no redirection of any kind: to keep output, use `spot.write`.

### Limits

| Limit | Default | |
|-------|---------|---|
| Time of one function | 5 s | then it is interrupted, or the host is killed |
| Memory of a mod host | 256 MB | a data limit (`RLIMIT_DATA`); `memory` in `[mod.<name>]` changes it |
| Rows in a value | 10,000 | larger values are refused |

Programs a mod starts get their normal memory limit back before they run: some, such as `node`, do
not start under the host's. Their memory is therefore **not limited**.

## Security

### The security contract

These are requirements, not intentions. **Code for mods is not accepted without a test for each rule
that tries to break it.** The sections after the list explain them.

**What is defended against:** data chosen by others (names, files, network and program output); a
parser or provider written by someone else, or changed by them after you approved it; another user on
the machine. **Not** against something already running as you or as root.

**Data and commands**

| # | Rule |
|---|------|
| S1 | No part of the API runs a command given as one string: there is no `spot.sh` |
| S2 | Every command of a pipeline needs permission, or none of them starts |
| S3 | Text from mods is data wherever it is shown: markup is escaped in dialogs, notifications and bar output, and control characters, terminal escape sequences included, are removed before printing to a terminal |

**What a mod can reach**

| # | Rule |
|---|------|
| S4 | A parser has no access to files, commands, network, environment, clock or its caller |
| S5 | A provider acts only through `spot`; `io`, `os`, `package`, `debug` and every way of loading code are absent (allow-list) |
| S6 | Nothing is granted without a permission that states its reason |
| S7 | A mod can never run `spot`, nor write the mods folder, spot's configuration, approvals or logs |
| S8 | File permissions are checked on the file that is actually opened, so that a symlink swapped in between does not pass |
| S9 | `spot.require` stays inside the mod's own folder, links included |
| S10 | A program is found in a fixed `PATH` of system folders. One in a folder your user can write, such as your own scripts, must be listed by full path; its hash is then part of the approval, and it is flagged |
| S11 | A program started by a mod gets a small environment (`HOME`, `LANG`, the runtime folder, the fixed `PATH`) plus the variables the mod has `env` permission for, **not the daemon's**, which may hold secrets |
| S12 | A program started by a mod inherits no open files, and never the channel to the daemon |

**Approval**

| # | Rule |
|---|------|
| S13 | No provider runs without approval; none is exempt |
| S14 | Approval needs a terminal and typing each dangerous permission; there is no `--yes` and no approve-all |
| S15 | Approval covers a hash of every file of the mod, its `mod.toml`, the parsers it uses and the programs of S10. What is hashed is what is loaded |
| S16 | A changed mod does not start, and you are told what is new |
| S17 | spot refuses a mods folder, mod file or approval record that is not yours or that others can write |
| S18 | spot never downloads or updates a mod |

**Daemon, hosts and clients**

| # | Rule |
|---|------|
| S19 | A host publishes and answers only under its own mod's name; the daemon enforces it, whatever the host says |
| S20 | Everything from a host is checked: size, form, names. Garbled messages kill it |
| S21 | Socket connections are accepted only from your own user, checked on both ends |
| S22 | Neither the daemon nor a mod host runs as root; they refuse to start if they would |
| S23 | A mod cannot exhaust spot: limits on time, memory, rows, message size, how many programs run at once, log size, and how often it can notify |
| S24 | Every command, file access and refusal is logged where mods cannot write, readable only by you |

**Requests are open to your programs.** Any program of yours can `spot call` any request of an
approved mod, so a request that does something drastic is a way for other programs to do it. A mod
must check the arguments it gets, and `spot mods show` lists the requests a mod offers.

#### Not promised

- Permissions control **which programs start**, not what those programs then do or touch.
- An approved mod can misuse what it was granted.
- A reason shown at approval may be a lie.
- spot, gopher-lua or the kernel may have bugs.

#### To be settled before coding

| Question | Why it matters |
|----------|----------------|
| Can Landlock bind a mod host and its programs to the `read` and `write` lists? | It would make those lists true for programs too. Untried |
| Should a `run` permission be able to limit arguments, for example `systemctl` only with `--user`? | `run = systemctl` alone grants a great deal |
| How are S8 and S12 done in Go? | Both are easy to get subtly wrong |
| Can approvals be writable only by root? | Raises the bar against a partly compromised account |

### Data must not become commands

A network name, a file name, a process name or a parser's result may be chosen by someone else.

- **There is no `spot.sh`.** No part of the API takes a command as one string, so data cannot grow
  into extra commands. Pipelines use `spot.pipe`.
- **A value can still be read as an option.** One starting with `-` changes what a program does, and
  with some, `ssh` for one, that can run other programs. Put `--` before such values, or check them.
- Arguments of `spot call` and values from dialogs arrive as strings in a list, never as shell text.
- A dialog's `run` takes no placeholders.

### A mod from someone else

A provider has no `io`, no `os` and no way to load code, so **everything it does goes through
`spot`, which can refuse**.

#### Permissions

Every permission comes with **the reason for it**; one without a reason makes the mod invalid.

```toml
[permissions.run]                    # programs it may start
systemctl = "start and stop the idle lock"

[permissions.read]                   # files and folders
"~/.ssh/config" = "list your SSH hosts"

[permissions.write]
[permissions.env]                    # variables spot.env may read
XDG_CURRENT_DESKTOP = "know which compositor runs"
```

- **Nothing is granted by default.** Anything not listed is refused, logged and **reported to you**:
  a mod that tries what it never asked for is exactly what you want to hear about.
- A system program is given and matched by bare name (`"ss"`) and found in a fixed `PATH`; a program
  your user can write, such as your own script, by its full path (S10). File permissions are matched
  after symlinks and `..` are resolved. The same rules hold for `spot.run` and
  for every command of `spot.pipe`.
- **`read` and `write` limit only `spot`'s own file functions, not the programs a mod runs.** A
  program does whatever your user can: a mod allowed to run `cat` can read any file, and one allowed
  `tee` can write any file, whatever `read` and `write` say.
- The `env` option of `spot.run` cannot set `PATH`, `LD_*` or similar variables, which would turn an
  allowed program into any program.
- **Never granted, whatever is listed:** running `spot`; writing to the mods folder, to spot's
  configuration, to its logs or to its record of approvals. A mod cannot widen its own permissions.

#### Approval

The point is to give you **the chance to notice**: a mod about keybinds that wants `ssh` should make
you ask why, before it ever runs.

```text
$ spot mods allow keybinds

keybinds 1.2.0 — "Lists your keybindings"
CHANGED since you approved it on 2026-11-26

  NEW  run  ssh      !! DANGER: reaches other machines
            the author says: "sync bindings"

       run  hyprctl     the author says: "read the live bindings"
       read ~/.config/hypr
                        the author says: "read your config"

  !! It can read files AND reach other machines: it could send them away.

Changed files: init.lua (+14 -2), mod.toml (+1)     see them: spot mods diff keybinds

Type the name of each dangerous permission to accept it (ssh), or press Enter to refuse:
```

- **What is new comes first**, and is marked. The reason is shown as the author's claim: it can be
  untrue.
- **Dangers are named**, from a list spot keeps: other machines and the network (`ssh`, `scp`,
  `curl`, `wget`, `nc`, `rsync`), everything (`sh`, `bash`, `fish`, `python`, other interpreters),
  more rights (`sudo`, `pkexec`), any file despite `read` and `write` (`cat`, `cp`, `mv`, `rm`,
  `tee`, `dd`, `tar`, `sed`, `awk`), private data (`~/.ssh`, keyrings, browser profiles), running code
  later (shell startup files, systemd user units, autostart, compositor config).
- **Combinations are pointed out**, such as reading private data together with reaching the network.
- **Accepting takes effort.** Each dangerous permission is accepted by typing its name; the prompt
  appears after a short wait and ignores what was typed before it. There is no `--yes`, no "approve
  all", and no approval without a terminal.
- **Read the code:** `spot mods show` prints a mod, `spot mods diff` what changed since your approval.
- The approval is **tied to a hash** of the mod's files, its `mod.toml` and the parsers it uses. After
  any change the mod does not start, and a notification says what is new: `keybinds changed and now
  asks to run ssh`. spot hashes and loads the same bytes.
- **No mod skips approval, your own included.** Your files can be changed by someone else.

#### Afterwards

`spot mods audit <name>` lists what a mod really did: each command with its arguments, each file read
or written, and everything refused, with times. The log cannot be written by mods.

#### What this does not do

**Permissions do not make a stranger's provider safe.**

- `read` and `write` do not bind allowed programs, as above.
- An allowed program can be abused: many (`systemctl`, `git`, `find`, …) can start others, and a
  file a mod may write can be a file that is run.
- A reason can be a lie, and a list of danger names is never complete.
- Warnings only help if they are read.
- Something already running as you can change spot, its approvals or your shell without any mod.
  These safeguards are against **mods**, not against an account that is already taken over.

Read a mod before approving it. A sandbox enforced by the kernel (Landlock, seccomp) would be a
stronger limit; it has not been tried.

### Who may talk to the daemon

Once mods can act, `spot call` makes the daemon's socket a way to cause actions. The daemon accepts
connections only from your own user, checked on the connection and not by file permissions alone,
and a client likewise checks who it is talking to.

## Using mod data

### In a dialog

| Key | On | Meaning |
|-----|----|---------|
| `from` | `list` | Rows come from the value. Rows with a `state` are switches |
| `from` | `switch`, `segmented`, `scale` | The value is the control's state (instead of `state`) |
| `call` | the same, and `button` | The request a change or a press makes (instead of `on_change` / `exec`) |

```toml
[[items]]
type  = "switch"
id    = "idle"
label = "Idle lock"
from  = "idle.enabled"
call  = "idle.set"        # called with the new value; a list row adds its key first
```

As with commands, values are read when the dialog opens and after every change made in it. A list
that updates by itself while open comes with live lists (0.11).

### From the command line

| Command | Does |
|---------|------|
| `spot get <mod.value>` | Print it once: text as it is, rows as `label<TAB>key<TAB>state`, tables as JSON |
| `spot watch <mod.value>` | Print it now and on every change, one per line, until killed |
| `spot call <mod.request> [args…]` | Run a request, print what it returns |
| `spot mods` | List mods: running, failing or disabled, with the last error |
| `spot mods reload [name]` | Load the files again |
| `spot parse <parser>` | Run a parser on standard input, print its records as a JSON array |
| `spot mods check [name] [-]` | Check a mod's contract and samples, or live input with `-` |
| `spot mods log <name>` | A mod's errors and log lines |
| `spot mods allow <name>` | Show a mod's permissions and dangers, and approve them |
| `spot mods show <name>`, `diff <name>` | Read a mod; what changed since you approved it |
| `spot mods audit <name>` | What it ran, read, wrote and was refused |
| `spot mods revoke <name>` | Withdraw the approval |

`--json` prints `get` and `watch` as one JSON value per line, which is what a bar wants.
`get`, `watch`, `call`, `parse` and `mods` become reserved words: a config of that name needs a path.
`watch` reconnects when the daemon is restarted, for instance after an upgrade.

## Health and errors

A broken mod must never just look like "nothing happened": started from a keybind, an error on
stderr is invisible. **The daemon keeps the health of every mod and makes sure you hear about it.**

- A dialog **always opens**. A broken mod affects only the items that use it.
- You are told **once, when it breaks**, not on every call.
- The message says **who is at fault**, what went wrong and where.

### Where you see it

| Place | What appears |
|-------|--------------|
| Desktop notification | When a mod starts failing or is disabled: `Parser "ss" broke its contract: record 17, field "key" is missing` |
| The dialog | In place of the data: the same message, which provider used it, and how to see details |
| A bar (`spot watch --json`) | `{ "text": "!", "tooltip": "…", "class": "error" }`: a warning, not a vanished module |
| `spot mods` | Every mod with its state, last error, `file:line`, how often and since when |
| `spot mods log <name>` | The history and the mod's own log lines |
| `spot get` / `call` / `parse` | Nothing on stdout, the message on stderr, a non-zero exit |

One notification per mod for each change from working to broken, none for repeats, and one when it
works again. They go to your notification daemon; `daemon.toml` can replace the command or turn them
off.

### Whose fault

| Broken | Blamed |
|--------|--------|
| A parser returns the wrong shape | the parser, naming who called it |
| Provider code errors, or passes a parser something that is not a string | the provider |
| A host dies, hangs or sends garbage | the mod running in it |
| A used parser is missing or does not load | the parser; its users show as waiting on it |

### A broken parser contract

| Who called the parser | What happens |
|-----------------------|--------------|
| `spot parse` | Nothing on stdout, the error on stderr, non-zero exit |
| A dialog (`parse = "ss"`) | The error under the item, no partial list |
| A provider (`spot.use`) | The call raises a Lua error, which the mod may catch |
| ...uncaught in a value | Readers keep the last good result, marked as old |
| ...uncaught in a request | The request fails, and a switch goes back to where it was |
| It does not even load (two functions, no `[returns]`) | Every mod that `uses` it is marked failing at load, not at first use |

A broken contract is a fault of the mod, not of its host: nothing is restarted.

### Broken messages between daemon and host

The other side is not trusted, all the less once any program can be a backend (0.12).

| Case | What the daemon does |
|------|----------------------|
| A line that is not JSON, or an unknown message | Kills and restarts the host: the stream cannot be trusted any more |
| A message over 16 MB | The same, without reading it all into memory (100,000 rows were about 7 MB) |
| A reply to a request nobody made | Ignores it and logs |
| A value of the wrong shape or with too many rows | Refuses that value; the host keeps running |
| No answer in time | Kills the host |
| A different protocol version, stated in the first message | Does not start the mod, and says so |

Garbled messages are fatal on purpose: skipping a bad line would let a half-broken mod look as if it
works.

### The rest

| Case | What happens |
|------|--------------|
| A mod is not approved, or changed since | It does not start; the notification says to run `spot mods allow` |
| A mod does something it has no permission for | The call fails as a mod error, naming what was refused |
| A mod fails to load | Marked failing; the others and all dialogs work |
| A `value` function errors | Readers get the last good result; `get` exits non-zero if there is none |
| Three errors in a row | The value stops being recomputed until the mod is reloaded or asked again after a pause |
| A function runs longer than 5 s | Interrupted inside the host; `spot.run` kills the process group |
| The host does not answer (stuck where it cannot be interrupted) | The daemon kills it and the commands it started, and starts a new one |
| A mod uses more than its memory limit | Its host dies; the daemon starts a new one |
| A host dies three times in a row | The mod is disabled until `spot mods reload` |
| A `from` names a mod that is not running | The dialog says so under the item |

A restarted mod starts empty. The daemon keeps the last values it published, marked as old, until new
ones arrive.

**Why a separate process.** Two things cannot be contained inside one program, as a test of the Lua
library showed:

- A slow string pattern runs in Go code that a time limit cannot interrupt: a short pattern took
  1.5 s on 200 characters and 23 s on 400.
- Running out of memory is fatal and cannot be caught: one oversized `string.rep`, or `s = s .. s`
  in a loop, ended the process.

Mods are isolated from each other in the same way: separate processes, sharing only what they
publish. Parsers a mod uses run in its process, so a bad parser takes that mod's host down with it.

## Trust

See [security](#security). A provider can do what you approved, and what the programs you allowed it
can do (which is more than its `read` and `write` list); the separate process protects the daemon from a broken mod, not you from a malicious one.

A parser is given no way to run or write anything and shares nothing with its caller, which is what
makes one from a stranger reasonable to use. What it returns is still data from that stranger: a
provider should not paste it into a shell command. It can also waste time and memory until its host
is killed, and with it the provider using it.

This protection is spot's, not the operating system's: it rests on the allow-list, the separate
state and the Lua library having no bugs. It has been checked by a few tests, not audited. Denying
parser-only hosts file access in the kernel (Landlock) is a possible second layer, not yet tried.

The daemon never runs as root; privileged actions go through your own helper (`sudo`, `pkexec`).

## Building it

- **The mod host** is a second, small program without GTK (`spot-mod-host`), shipped with spot. The
  packages and the release have to carry both.
- A new package `internal/mods`, without GTK, used by the host and by `internal/daemon`.
- Lua: [gopher-lua](https://github.com/yuin/gopher-lua), written in Go, so no new C dependency.
- The socket protocol gains `get`, `watch` and `call`. `watch` answers many times on one
  connection, where today every request has one answer.
- `daemon.toml` gains `mods`, `mods_dir` and `[mod.*]`. Older versions of spot reject unknown keys
  there and fall back to their defaults with a warning.

### One model for mods and backends

A mod host and a long-running **external backend** (0.12) are the same thing to the daemon: a
process that publishes values and answers requests in JSON lines. A mod host is simply a backend
that runs Lua.

```json
{"publish": "table", "data": [{"label": "tcp LISTEN", "key": "0.0.0.0:22"}]}
{"request": 7, "name": "set", "args": ["false"]}
{"reply": 7, "ok": true, "data": false}
```

`from`, `call` and the command line do not know which of the two is behind a name.

### What was tested

With gopher-lua 1.1.2, in a throwaway program, not in spot:

| Question | Result |
|----------|--------|
| Can a runaway script be stopped? | Yes for Lua code, including loops that catch errors, coroutines and callbacks; **no** inside a slow pattern |
| Cost of a time limit | About 10 % |
| A parser like the one above on 5,000 lines | 65 ms |
| A parser loaded into its caller's state | could use the caller's `spot`; with own globals it still replaced the caller's `string.format`; in a separate state, neither |
| An allow-list of globals | left only the names listed above |
| Copying rows from a parser's state to its caller's | 100: 0.2 ms; 1,000: 1.2 ms; 10,000: 15 ms |
| A new Lua state | 40 µs |
| A separate process running Lua | about 7 MB; 1 ms to start, run and exit |
| Limiting its address space (`RLIMIT_AS`) | unusable: Go failed normal work even at 1 GB |
| Limiting its data (`RLIMIT_DATA`) to 256 MB | stopped three kinds of runaway allocation within 0.2 s; normal work ran from 64 MB up |
| Commands under an inherited limit | `node` failed at 256 and 512 MB; with only the soft limit set and restored for the command, it ran |
| A systemd scope instead | also works; needs systemd and starts slower (46 ms against 27 ms) |
| Checking memory from outside every 20 ms | overshot to 354 MB against 256 |
| Passing rows between processes (send and decode) | 100: 0.2 ms; 1,000: 1.8 ms; 10,000: 16 ms; 100,000: 145 ms, the sender peaking at 318 MB |

The limit tests were on Linux. The transfer figures cover one hop, not drawing the list.

**Still to test:** the real host's size and start time.

### First version

In: the mod host with its time and memory limits, loading and enabling, the `parser` and `provider` types, the parser contract and lockdown, `spot.use`, `spot parse`, `spot mods check`, mod health and notifications, permissions with reasons, approval and the audit log, the socket checks, `run` / `parse` in dialogs, `value` / `publish` / `refresh` / `on`, the helpers above except `watch_file`,
`get` / `watch` / `call` / `mods`, and `from` / `call` in dialogs.

Later: `watch_file`, built-in system events (power, network), automatic reload on save.

The API is **experimental** until menu trees (0.10) have used it.

## Open questions

1. **Lua version.** gopher-lua is Lua 5.1 (as in Neovim). Is that enough, or is 5.4 worth a C
   dependency?
2. **Keys in TOML.** Are `from` and `call` clear, next to the existing `state`, `on_change`, `exec`
   and `[[sources]]`?
3. **Enabling.** A list in `daemon.toml`, or a file per mod? Should a dialog config be able to ask
   for a mod?
4. **Sharing mods.** Should there be a convention for installing someone else's mod (a git folder
   in `mods/`), or is that out of scope?
5. **`io` and `os`.** Providers start with `spot`'s helpers only. Is anything missing from them, and
   is there a Lua library you need that expects `io` or `os`?
6. **No shell.** `spot.run` and `spot.pipe` take lists. Is there something your scripts do that
   they cannot express?
7. **Parsers in dialogs.** Are `run` and `parse` the right keys for using a parser without a
   provider, and is that wanted in the first version?
   Is the [contract](#the-parser-contract) enough to share parsers by, or is something missing, such
   as declared options?
8. **Types.** Are parser and provider enough, or is a third kind needed, such as one that only
   performs actions?
9. **One host per provider, or one for all?** Separate hosts isolate better and cost about 7 MB
   each in the test.
10. **Limits.** Are 256 MB of memory and 10,000 rows the right defaults?
11. **For whoever asked for modding:** what would you build first, and can this API do it?
