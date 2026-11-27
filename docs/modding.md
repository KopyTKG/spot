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

[permissions.netlink]
diag = "ss asks the kernel for the sockets"
```

Enable the provider in `~/.config/spot/daemon.toml`, and approve what it asks for. The parser it uses
comes with it:

```toml
mods = ["sockets"]
```

```sh
spot mods allow ss          # a parser: no permissions, confirm after reading
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
"custom/sockets": { "exec": "spot watch --waybar sockets.count", "return-type": "json" }
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
data, refresh it or answer requests. A parser named by a dialog or `spot parse` must be approved like
any mod (S13), and runs in a mod host started for that one call.

### Locked down

- **Its own Lua state.** Text and options are copied in and plain data is copied out. No function,
  table or `spot` is ever shared with the mod that calls it. This is the barrier: loaded into the
  caller's state instead, a test parser wrote a file through the caller's `spot.write`, and even
  with its own table of globals it replaced the caller's `string.format`.
- **An allow-list of globals**, below. What is not on it is not there.
- **No way out:** no commands, files, network, environment, timers or publishing.
- **A fresh state for every call**, nested `spot.use` calls included, so nothing is kept between
  calls and no parser can change another. A chain of `uses` may not be circular or deeper than 8.
- **Same input, same output:** no clock, no `math.random`, and `tostring` of a table or function shows
  no address. The order of `pairs` over a map is not defined; a parser must not rely on it.
- **Copying is raw.** Output and error objects are read without metamethods, so no parser code runs
  while they are checked.
- `spot.use` validates a name before it becomes a path.
- Its `spot` has only `spot.use` (other parsers, each in a state of its own), `spot.json` and
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
| Enabled | a provider: listed in `mods` in `daemon.toml` **and** approved with `spot mods allow`. A parser: approved, and used by something. Otherwise it never runs |
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
- `spot` is not a table but a userdata with read-only access: assigning to it fails, `rawset` cannot
  touch it, and its metatable is hidden. (A table guarded by a metatable is still open to `rawset`.)
- The Lua state is created with no libraries opened (not opened and then removed), a bounded call
  stack and registry, and metatables of what spot hands out cannot be reached.
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
| `spot.run(argv, options)` | `{ status, stdout, stderr }`. `argv` is a list; **there is no shell**. Options: `timeout` (ms, default 5000), `stdin`, `env` (only what `setenv` lists), `dir` (inside `read`) |
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
| Options | `timeout` for the whole pipeline, `stdin` for the first command, `env` and `dir` for all, under the same permissions as `spot.run` |
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
| Published values per mod | 100 | starting values, still to be measured |
| One field | 4 KB | |
| Kept per mod | 32 MB | |
| Watchers per value | 64 | |
| Programs at once, bytes written | capped per mod | with cgroups, also tasks and CPU |
| Output kept from a command (`stdout`, `stderr`) | 8 MB | the program is killed beyond it |
| Size of a file read | 8 MB | |
| Timers per mod | 64 | |
| Notifications per mod | a few per hour | so that failing and recovering in turn cannot flood |
| Waiting output per watcher | capped | a watcher that does not read is dropped |

Programs a mod starts get their normal memory limit back before they run, set by the program's own
launcher step in the child (the host's limit is never raised): some, such as `node`, do
not start under the host's. Their memory is therefore **not limited**.

### Processes

| | |
|---|---|
| Stopping | A cgroup per mod **and** a child subreaper: `cgroup.kill` misses a program that moved itself to another cgroup, the subreaper finds it. The adopting process is the daemon or a supervisor per mod, never the host that is killed. A job handed to `systemd` is not stopped by either |
| Cgroups | Need delegation (`Delegate=yes`), which a daemon started by a compositor's autostart does not have unless started through `systemd-run --user --scope` or a user service. Without it only the subreaper, and `spot mods` says so |
| Resource limits | With delegation, `pids.max` and `memory.max` on a mod's cgroup would cover its programs too (settable in a test; enforcement not yet tried). Inside the sandbox a program cannot move itself out (it cannot write `/sys/fs/cgroup`; untested). Without delegation, ask the user's `systemd` for a scope per mod (`MemoryMax`, `TasksMax`); without that, programs have **no** memory or process limit |
| Channel | A socket pair, not a pipe: a program could write into a pipe through `/proc` |
| Open files | All beyond stderr are marked close-on-exec (`close_range`) before any program starts |
| Reading each other | Mod hosts set `PR_SET_DUMPABLE` to 0. The daemon **cannot**: tested, the desktop portal then refuses it the desktop's settings. The sandbox already keeps a mod's programs from reading it (tested for a parent process) |
| Environment | Hosts start with the small environment of S11 |

## Security

### What a permission promises

**Permissions are enforced by the kernel.** Each mod host runs in a [sandbox](#the-sandbox), Landlock
**and** a seccomp filter, set up before any mod code is read, which every program it starts inherits and
none can lift. Files, the network and
sockets are what was granted, for the mod's Lua and for its programs alike.

**With two exceptions, and both are shouted at approval:**

- A **socket permission** hands the mod to whatever listens there. The user's D-Bus and Hyprland can
  run any command for whoever asks, so granting either one **means the mod can do anything you can**.
- **Write access to something that runs later** (shell startup files, autostart, systemd user units,
  compositor config, …) runs the mod's code outside the sandbox.

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
| S25 | Data from `from` / `parse` never becomes part of shell text, [`[submit]`](#values-in-submit) included |
| S30 | A value cannot add a row or a field to text output |
| S31 | A mod's rows cannot name an image file |

**What a mod can reach**

| # | Rule |
|---|------|
| S4 | A parser has no access to files, commands, network, environment, clock, its caller or another parser, and keeps nothing between calls |
| S5 | A provider acts only through `spot`; `io`, `os`, `package`, `debug` and every way of loading code are absent (allow-list) |
| S6 | Nothing is granted without a permission that states its reason |
| S7 | Neither a mod nor a program it starts can write the mods folder, spot's configuration, approvals or logs, or run `spot` to any effect: it cannot reach the daemon |
| S8 | File permissions are checked on the file that is actually opened, so that a symlink swapped in between does not pass |
| S9 | `spot.require` stays inside the mod's own folder, links included |
| S10 | A program is found in a fixed `PATH` of system folders. One in a folder your user can write, such as your own scripts, must be listed by full path; its hash is then part of the approval, and it is flagged |
| S11 | A program started by a mod gets a small environment (`HOME`, `LANG`, the fixed `PATH`; the runtime folder only with a `socket` permission) plus the variables the mod has `env` permission for, **not the daemon's**, which may hold secrets |
| S12 | A program started by a mod inherits no open files, and can neither reach the channel to the daemon nor read its host's or the daemon's environment |
| S26 | A program gets only the variables of S11 and those listed in `setenv` |
| S27 | A program's working folder stays inside what the mod may read |
| S42 | What a mod passes to `spot` is copied once, without metamethods, and checked as copied; numbers that are not finite, lists with holes and deep nesting are refused |
| S28 | File functions handle only regular files; none under `/dev`, and under `/proc` and `/sys` only paths listed in `read`, never a process's own folder, never for writing |

**Approval**

| # | Rule |
|---|------|
| S13 | No mod runs without approval, parsers included; none is exempt |
| S14 | Approval needs a terminal and typing the name of **every** `run` entry and every new or dangerous permission; there is no `--yes` and no approve-all |
| S15 | Approval covers a hash of every file of the mod, its `mod.toml`, every parser it uses directly or through another, and the programs of S10. What is hashed is what is loaded, from memory, not read again from disk |
| S16 | A changed mod does not start, you are told what is new, and what it had published is thrown away |
| S32 | Text from a mod cannot change how the approval, `show`, `diff`, `log` or `audit` read |
| S38 | A mod is scanned before approval by reading its code, never by running it, and the same bytes are then hashed and loaded |
| S39 | The scanner never calls a mod safe; flagged or not, you are told to read it |
| S40 | `spot` appears in a mod only as what is being called (`spot.run(…)`, `spot.json.encode(…)`) or as `spot.config`; `_G` not at all; `spot.run` / `spot.pipe` get their list written out in the call, with plain entries only and the program first as a literal string. A mod that does otherwise is refused |
| S17 | spot refuses a mods folder, mod file, approval record, `daemon.toml`, dialog that uses mods, system `PATH` folder or host binary that others than you or root can write, checking parent folders and symlinks too. The host binary is the one beside spot's own, never one found in `PATH` |
| S18 | spot never downloads or updates a mod |

**Daemon, hosts and clients**

| # | Rule |
|---|------|
| S19 | A host publishes and answers only under its own mod's name; the daemon enforces it, whatever the host says |
| S20 | Everything from a host is checked: size, form, names. Garbled messages kill it |
| S21 | Socket connections are accepted only from your own user, checked on both ends; the daemon also refuses one from a mod host or anything it started |
| S22 | Neither the daemon nor a mod host runs as root or with capabilities (any of uid, euid, gid, egid 0, or an effective capability); they refuse to start |
| S23 | A mod cannot exhaust spot: the [limits](#limits) on time, memory, rows, values, message size, programs, bytes written, log size and notifications |
| S24 | Every `spot` call (command, file access) and every refusal by spot is logged where mods cannot write, readable only by you |
| S29 | Stopping a mod stops what it started |

**The sandbox**

| # | Rule |
|---|------|
| S33 | Every mod host puts itself under the sandbox, and checks that it is in it, before reading any mod code. Where the kernel cannot provide it (Landlock ABI 11 or newer, and seccomp), no mod runs |
| S34 | No network without a permission, by any protocol: Multipath TCP, ICMP and VM sockets included. A permission allows connecting out to the listed ports; no mod accepts connections |
| S35 | No Unix socket without a `socket` permission; the daemon's own socket is never granted, nor a folder that contains it |
| S36 | A `read`, `write` or `socket` grant that contains spot's socket, or a `write` grant that contains its configuration, mods, approvals or logs, is refused: the kernel can only add access, not carve an exception out of it |
| S37 | The base grant is the short list in [the sandbox](#the-base-grant); it does not include all of `/etc`, `/usr/bin`, your home or `/run`. Programs are granted one by one, from `run`, and libraries may be read, not executed |
| S43 | A host whose file grant no longer matches the file, because an update replaced it, is restarted with new grants before it is asked anything else |
| S41 | A seccomp filter allows sockets by list, netlink only for the granted families, and denies what Landlock does not cover: changing modes, owners and extended attributes, shared memory and message queues, kernel keyrings, running a program from memory, new namespaces and mounts, `io_uring`, looking into other processes, and `listen` for a mod with network access |

**Requests are open to your programs.** Any program of yours can `spot call` any request of an
approved mod, so a request that does something drastic is a way for other programs to do it. A mod
must check the arguments it gets, and `spot mods show` lists the requests a mod offers.

S29 holds fully only with cgroups; without them, the tests and `spot mods` say what is not covered. A
job handed to `systemd` needs the user bus, which the sandbox refuses unless granted.

#### Not promised

- With a `socket` permission, or write access to something that runs later, a mod can leave the
  sandbox.
- **Not only the listed programs can run.** `/usr/lib` must be readable for libraries, and it holds
  programs too (525 binaries on the tested machine). The dynamic loader starts any of them, and any
  program in a folder the mod may write. A mod's Lua cannot ask for that (`spot.run` takes listed
  programs only), but a listed program that starts others, such as a shell, can. They run in the same
  sandbox and gain no access by it. Programs in `/usr/bin` that are not listed cannot even be read.
- A network permission is **by port, not by destination**: port 443 reaches any machine, and a port
  also reaches whatever listens on **this machine** and your local network, such as a development
  server that trusts local callers.
- **Any network permission can send data anywhere**, whatever port it names: name lookups need UDP
  port 53, and that reaches any server. A mod's UDP socket also receives from whoever sends to its
  port.
- A mod can see that a file exists and its size and times (`stat`) anywhere you can; the kernel does
  not restrict that.
- The audit log records what goes through `spot`. What a program a mod started opens or is refused
  is not in it.
- Programs that change file modes, such as `cp -p` and `install -m`, fail under the filter.
- An approved mod can misuse what it was granted, and a reason shown at approval may be a lie.
- spot, gopher-lua or the kernel may have bugs.

#### To be settled before coding

| Question | Why it matters |
|----------|----------------|
| Does the base grant hold on other distributions, whose libraries lie elsewhere? | Only Arch is tested |
| Do very large DNS answers (which need TCP port 53) and `.local` names have to work? | Both are untested and would likely fail |
| How should the daemon be started so that it has a delegated cgroup? | Decides how far S29 and the process limits hold |
| How is one of your own scripts run without being swapped between hashing and starting it? (a sealed in-memory copy is one idea; scripts with `#!` and their imports are the difficulty) | S10 promises the approved bytes |
| Should a security switch (a lock, a firewall) be able to say "unknown" instead of showing the last value when its mod is down? | A stale "on" is worse than no answer |
| Should a `run` permission be able to limit arguments? | Less pressing now that the sandbox holds, but it narrows what a granted socket gives |
| Can approvals be writable only by root? | Raises the bar against a partly compromised account |
| Should unquoted `{id}` for `stdin = true` lists be turned off unless a config asks for it? | It is a way into a shell today; changing it breaks existing configs |

### The sandbox

[Landlock](https://docs.kernel.org/userspace-api/landlock.html), a Linux kernel feature that lets a
process give up access for itself and everything it starts, without root.

**Mods need Landlock ABI 11 or newer**, with `landlock` among the kernel's active security modules.
That is the only version the sandbox has been tested on (Linux 7.3). The minimum is lowered only after
testing an older kernel. The rest of spot does not need Landlock. To see what a machine has:

```sh
cat /sys/kernel/security/lsm
python3 -c 'import ctypes; print(ctypes.CDLL(None).syscall(444, 0, 0, 1))'   # the ABI
```

- **The host restricts itself, then executes itself again**, before it reads any mod code. Landlock
  and the filter bind one thread and a Go program has many, so the thread is locked from the first
  restriction to the exec, and the filter is also installed on all threads (`TSYNC`). What the
  executing thread has then covers the new program. The daemon is never restricted: doing it there
  would leave some of its threads sandboxed.
- **The second stage proves it is restricted before it reads anything**, and exits otherwise:
  `no_new_privs` is set, a VM socket is refused (the filter) and a file outside the base grant cannot
  be opened (Landlock). Starting that stage by hand therefore runs no mod.
- **The filter (seccomp)** allows Unix sockets (ruled by Landlock); with `net`, plain TCP, and UDP for
  name lookups; with `netlink`, only the granted families (`diag` does not open `route`). Every other
  socket family and protocol is refused. **With `net` it also denies `listen`**: Landlock rules
  `bind`, but a TCP socket that was never bound can still `listen`, on a port the kernel picks and on
  every interface. It answers `clone3` and `pidfd_open` with "not implemented",
  so that callers fall back, refuses calls of another architecture, and denies:

  | Denied | Why |
  |--------|-----|
  | `chmod`, `fchmod`, `fchmodat`, `fchmodat2`, `chown` and relatives, extended attributes | Landlock does not cover them |
  | SysV shared memory, semaphores and queues; POSIX message queues | two mods could pass secrets: one that may read to one that may connect |
  | `add_key`, `request_key`, `keyctl` | the same, and your Kerberos or filesystem keys may be there |
  | `memfd_create`, `memfd_secret`, `execveat` | a program run from memory needs no file anyone could see |
  | new user namespaces, `unshare`, `setns`, mounts, `chroot`, `pivot_root` | |
  | `ptrace`, `process_vm_readv` / `writev`, `pidfd_getfd`, `kcmp`, `open_by_handle_at` | reaching into other processes and past paths |
  | `io_uring`, `bpf`, `userfaultfd`, `perf_event_open` | large kernel surface that a mod has no use for |

- Landlock ABI 11 limits UDP by port as well as TCP, so UDP is granted for port 53 only (to any
  address). Sending also needs a rule for binding port 0. TCP Fast Open and addressing another port
  from a connected socket are both covered by the port rules.
- What a host gets is the [base grant](#the-base-grant) and what its permissions list. **Not the mods
  folder**: the daemon reads every file once, checks the hash and sends the bytes to the host.
- No other process can be read, traced or signalled, abstract sockets included.
- `sudo` and `pkexec` do not work: a sandboxed process cannot gain rights.
- Parser hosts, also those started for `spot parse` or a dialog, get their own program file and
  nothing else, so even a bug in the Lua restrictions leaves them with nothing to reach.

#### The base grant

| For | Granted |
|-----|---------|
| every host | **its own program file** (execute and read). A host that runs no programs needs nothing else |
| a mod with `run` | `/dev/null`; the dynamic loader (execute); `/usr/lib` (**read, not execute**) |
| correct local time | `/etc/localtime`, `/usr/share/zoneinfo` (read) |
| user and group names | `/etc/passwd`, `/etc/group` (read) |
| each entry of `run` | that program, and its interpreter if it is a script (execute) |
| a mod with `net` | TCP to the listed ports; UDP to port 53 and binding port 0; `/etc/resolv.conf`, `/etc/hosts`, `/etc/ssl/certs`, `/etc/ca-certificates` (read) |

Nothing else of `/etc` is included: no tested program needed `ld.so.cache`, `nsswitch.conf`, the
terminal descriptions, `/dev/urandom` or translations; name lookups and HTTPS did not need
`nsswitch.conf` or `openssl.cnf` either. `/etc/hosts` was not needed with `systemd-resolved`; it is
included for resolvers that read it themselves. Lookups do not go through the socket of
`systemd-resolved`: programs that resolve names on their own, such as Go programs, cannot use it. What a program needs beyond this, such as `/proc` for `ps`,
`/sys` for `brightnessctl` or its own configuration file, is a permission the mod asks for.

**An update can take a grant away.** Landlock binds a rule to the file, not to its path. When a file
granted by name is replaced by renaming another over it, as package updates do, the running host is
refused the new one. Folder grants are not affected. So the daemon remembers which file each name
meant when the host started, compares before it asks the host anything, and **restarts the host** when
one changed (S43). This is not a failure of the mod and is not reported as one. A program you wrote is
a different matter: its approved hash no longer matches (S10).

### Data must not become commands

A network name, a file name, a process name or a parser's result may be chosen by someone else.

- **There is no `spot.sh`.** No part of the API takes a command as one string, so data cannot grow
  into extra commands. Pipelines use `spot.pipe`.
- **A value can still be read as an option.** One starting with `-` changes what a program does, and
  with some, `ssh` for one, that can run other programs. Put `--` before such values, or check them.
- Arguments of `spot call` and values from dialogs arrive as strings in a list, never as shell text.
- A dialog's `run` takes no placeholders.

### Values in `[submit]`

`[submit]` puts `{id}` into `sh -c` **unquoted**, and with `from` or `parse` the value can be chosen
by someone else. So for items filled by `from` or `parse`:

- a config that uses their `{id}` in `submit.exec` or `spawn` is **refused**;
- the value is passed in the environment instead, as `SPOT_VALUE_<ID>`:

```toml
[submit]
exec = 'ping -c 1 -- "$SPOT_VALUE_HOST"'
```

Quoting automatically would not be enough: inside a template's own `"{id}"`, `$(...)` would still
run. Two ids that map to the same variable name are a config error, and a value containing NUL is
refused. Lists with `stdin = true` have the same problem today; changing them would break existing
configs, so they get the variables too and the documentation warns.

- Every `SPOT_VALUE_*` inherited from the caller is removed first, so that an empty item cannot take
  someone else's value; item ids match `[a-zA-Z][a-zA-Z0-9_-]*`.
- A value from `from` / `parse` that starts with `-` is refused unless its item sets
  `allow_leading_dash = true`. Examples put `--` before values where the program supports it.

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

[permissions.setenv]                 # variables it may set for the programs it runs
LC_ALL = "get untranslated output"

[permissions.net]                    # none by default; TCP ports it may connect to
"443" = "fetch the weather"

[permissions.netlink]                # the kernel's tables: "diag" (sockets), "route" (interfaces)
diag = "list the open sockets"

[permissions.socket]                 # dangerous: see above
"$XDG_RUNTIME_DIR/hypr" = "read the live key bindings"
```

- **Nothing is granted by default.** Anything not listed is refused, logged and **reported to you**:
  a mod that tries what it never asked for is exactly what you want to hear about.
- A system program is given and matched by bare name (`"ss"`) and found in a fixed `PATH`; a program
  your user can write, such as your own script, by its full path (S10). File permissions are matched
  after symlinks and `..` are resolved. The same rules hold for `spot.run` and
  for every command of `spot.pipe`.
- **`read` and `write` bind the programs too.** A mod allowed to run `cat` reads only what it may
  read. A grant of a folder covers everything below it.
- The `env` option of `spot.run` / `spot.pipe` sets only variables listed in `setenv`. `LANG`,
  `LC_*`, `TZ`, `NO_COLOR` and `TERM` are ordinary; **any other name is a danger**, accepted by typing
  it, since many variables make a program load or run something else (`GIT_SSH_COMMAND`,
  `PYTHONSTARTUP`, proxy and certificate settings, …). Never set, whatever is listed: `PATH`, `HOME`,
  `LD_*`, `GLIBC_*`, `GCONV_PATH`, `LOCPATH`, `NLSPATH`.
- The `dir` option must be inside a `read` entry. By default a program starts in an empty folder
  owned by spot.
- **Never granted, whatever is listed:** the daemon's socket; writing to the mods folder, to spot's
  configuration, to its logs or to its record of approvals, or to a folder containing any of them.

**Files.** Regular files only, opened non-blocking and checked on the open descriptor, with a size
cap. `/dev` is refused. spot's protected files are recognised by device and
inode as well as by path. **A file with more than one hard link is not written, and not read unless
it belongs to root and you cannot write it** (as in some systems' package stores): a link inside a
granted folder reads the file it shares, wherever else that is. `spot.write` writes a new file and
renames it over the old one after checking, never truncating first. `spot.exists` and `spot.list`
check `read` themselves, since the kernel does not rule `stat`. Opening is `openat2`
with `RESOLVE_NO_SYMLINKS` on the resolved path, then a check of the descriptor. Size limits count the
bytes read: files under `/proc` report a size of 0.

**`/proc` and `/sys`** are where battery, network and load are read, so they are not closed, but
each path must be listed, and only for reading:

```toml
[permissions.read]
"/sys/class/power_supply" = "know whether the laptop is on battery"
```

- A broad entry (`/sys` or `/proc` itself) is refused: name what is needed.
- `/proc/self`, `/proc/thread-self` and every `/proc/<number>` are always refused: they hold
  processes' environments and open files.
- **What is granted is the listed folder and the real folders its direct entries point to, no
  further.** The entries of `/sys/class/power_supply` are links into `/sys/devices`, and those are
  included; the links inside them (`device`, `subsystem`) are not, since following them reaches all
  of `/sys`.
- `/proc/net` and `/proc/mounts` lead into a process's own folder. They are named exceptions **for
  `spot.read` only**, which give the mod host's own view: a program the mod starts has a folder of its
  own and is refused.
- Nothing under `/proc` or `/sys` can be written.

#### Approval

The point is to give you **the chance to notice**: a mod about keybinds that wants `ssh` should make
you ask why, before it ever runs.

```text
$ spot mods allow keybinds

keybinds 1.2.0 — "Lists your keybindings"
CHANGED since you approved it on 2026-11-26

  NEW  socket  $XDG_RUNTIME_DIR/hypr     !! DANGER: Hyprland runs any command it is
                                            asked to. This mod can do anything you can.
               the author says: "read the live bindings"

  NEW  net     443                       !! DANGER: can reach any machine on this port
               the author says: "check for updates"

       run     hyprctl                   the author says: "ask Hyprland"
       read    ~/.config/hypr            the author says: "read your config"

  !! It can read files AND reach the network: it could send them away.

Changed files: init.lua (+14 -2), mod.toml (+4)     see them: spot mods diff keybinds

Type the name of each new or dangerous permission to accept it (socket, net, hyprctl),
or press Enter to refuse:
```

- **What is new comes first**, and is marked. The reason is shown as the author's claim: it can be
  untrue.
- **Dangers are named:** any `socket`, any `net`, `netlink`, an unusual `setenv` name, an `env` name
  that looks like a secret (`*_TOKEN`, `*_KEY`, `*_SECRET`, `*_PASSWORD`, `SSH_AUTH_SOCK`), interpreters and shells (they run whatever they are
  given, inside the sandbox), programs that reach other machines, private data (`~/.ssh`, keyrings, browser profiles), running code
  later (shell startup files, systemd user units, autostart, compositor config, `mimeapps.list`,
  `~/.local/share/applications`, D-Bus service files, git config).
- **Combinations are pointed out**, such as reading private data together with reaching the network.
- **Accepting takes effort.** Every `run` entry and every dangerous permission is accepted by typing its name; the prompt
  appears after a short wait, is read from the terminal itself (`/dev/tty`) and discards what was
  typed before it. There is no `--yes`, no "approve all", and no approval without a terminal.
- **A mod cannot dress up the screen.** A reason is one line of at most 200 characters. Bidirectional
  and invisible characters are removed from reasons and descriptions. `show`, `diff`, `log` and
  `audit` print every control, bidirectional and invisible character **visibly** (`\x1b`,
  `\u{202e}`), in code too: removing them would hide what the reader should see.
- **Only the daemon reads approvals.** It finds them, and spot's configuration, from your home folder
  in the system's user database, not from environment variables: a daemon started on demand inherits
  the environment of whoever called, which could otherwise point it at another record.
- **Revoking or changing a mod stops it**, with everything it started.
- **Read the code:** `spot mods show` prints a mod, `spot mods diff` what changed since your approval.
- The approval is **tied to a hash**: SHA-256 over a sorted list of every file (path, length, digest),
  whether there is a `mod.toml`, the lists of all parsers used, and the programs of S10; one approved
  hash per mod. A parser has no permissions, so approving one is reading what is flagged and
  confirming. After
  any change the mod does not start, and a notification says what is new: `keybinds changed and now
  asks to run ssh`. spot hashes and loads the same bytes.
- **No mod skips approval, your own included.** Your files can be changed by someone else.

#### The scanner

Before you approve a mod, spot reads its code and **points at things that are known to be used for
harm, and also have honest uses**. It decides nothing: it shows you where to look.

```text
$ spot mods allow keybinds
...
Flagged in the code (each also has honest uses):

  init.lua:14  reads "~/.ssh/id_ed25519"
               a private key; tools that list SSH hosts need ~/.ssh/config, not keys
  init.lua:15  runs curl
               can send data to other machines; honest use: fetching information
  init.lua:15  contains the address "https://example.invalid/collect"
  init.lua:31  a long encoded string
               can hide what the code does; honest use: an embedded icon

Read the mod before you enable it:  spot mods show keybinds
```

With nothing to point at, it says: **"Nothing flagged. This is not a safety check: read the mod before
you enable it."** It never says that a mod is safe or clean.

| Flagged | |
|---------|---|
| Programs | those that reach other machines, shells and interpreters, `sudo` and `pkexec`, `systemctl`, `crontab`, `chmod`, `base64`, `dd` and the like |
| Paths | keys, keyrings, browser profiles; places that run code later |
| Text | addresses and IP numbers, long encoded or escaped strings, text assembled from character codes |
| Permissions | calls with no permission for them, and **permissions asked for but never used** |
| Shape | arguments that are only known when the mod runs, `unpack` among later arguments of `spot.run`, `string.reverse`, paths assembled from pieces |

Each finding gives the file and line, why it can be misused and what it is honestly used for. The
list lives in spot and grows; a mod cannot change it or switch a finding off. The same scan runs in
`spot mods check`, so authors and CI see it, and parsers are scanned like providers.

**The strict form (S40)** is what makes scanning worth something. Without it, copying `spot` into a
variable and building a name from pieces hid everything from a test scanner. Allowing `spot.name` as a
value was still not enough: `pcall(spot.run, …)`, or handing `spot.run` to another function, hid the
program again. With `spot` only ever written where it is called, which of spot's functions a mod can call, and which programs its Lua starts,
is known before it runs.

For programs this means the list itself is literal: `spot.run({ "ss", "-tun", port })`. A list built
elsewhere, a keyed entry (`[1] = …`) or `unpack` in first place is **refused**, not just flagged. A
duplicate key shows why: `{ [1] = "ss", [1] = x }` runs `x`. It is a rule about how code is written, not a judgement of what it does.

Arguments can still be built while the mod runs, and a scanner can be fooled. **The sandbox is the
protection; the scanner helps you notice what to ask about.**

#### Afterwards

`spot mods audit <name>` lists what a mod really did: each command with its arguments, each file read
or written, and everything refused, with times. `spot`'s own functions cannot write it. It has mode
0600, is rate limited with a count of what was dropped, and never records `stdin` or environment
values. It records what goes through `spot`, not what a started program does by itself.

#### What this does not do

The sandbox limits what a mod can reach. It does not make a mod honest: within what you granted, it
can do the wrong thing, and with a socket or a write to something that runs later it can do anything.
Warnings only help if they are read. Something already running as you can change spot, its approvals
or your shell without any mod: these safeguards are against **mods**, not against an account that is
already taken over. Read a mod before approving it.

### Who may talk to the daemon

Once mods can act, `spot call` makes the daemon's socket a way to cause actions. The daemon accepts
connections only from your own user, checked on the connection and not by file permissions alone,
and a client likewise checks who it is talking to. With mods, the socket moves into a folder of its
own (`$XDG_RUNTIME_DIR/spot/`, mode 0700), so that no grant has to include it, and the daemon refuses
a peer that is a mod host or something it started.

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

A mod's rows may name an icon from the **icon theme** only: a value containing `/` is ignored.

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
| `spot mods check [name] [-]` | Check a mod's contract, samples and strict form, and scan it; live input with `-` |
| `spot mods log <name>` | A mod's errors and log lines |
| `spot mods allow <name>` | Show a mod's permissions and dangers, and approve them |
| `spot mods show <name>`, `diff <name>` | Read a mod; what changed since you approved it |
| `spot mods audit <name>` | What it ran, read, wrote and was refused |
| `spot mods revoke <name>` | Withdraw the approval |

`--json` prints `get` and `watch` as one JSON value per line, unaltered, for scripts. `--waybar` is for
a bar, which reads markup: `text` and `tooltip` are escaped and `class` is limited to `[a-z0-9_-]`. In text
output, piped or not, backslash, tab and newline are escaped, so that a value cannot forge a row or a
field.
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
| A bar (`spot watch --waybar`) | `{ "text": "!", "tooltip": "…", "class": "error" }`: a warning, not a vanished module |
| `spot mods` | Every mod with its state, last error, `file:line`, how often and since when |
| `spot mods log <name>` | The history and the mod's own log lines |
| `spot get` / `call` / `parse` | Nothing on stdout, the message on stderr, a non-zero exit |

One notification per mod for each change from working to broken, none for repeats, and one when it
works again; revoked or changed mods have their values removed at once. They go to your notification daemon, as arguments or environment variables, never pasted into a command; `daemon.toml` can replace the command or turn them
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
| A file granted by name was replaced (a system update) | The host is restarted with new grants; nothing is reported |
| A host keeps dying | It is restarted with growing pauses (2 s, 10 s, 1 min, then every 5 min), and you are told once. Not disabled for good: data from outside, such as a network name that trips a slow pattern, must not be able to switch a mod off |
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

See [security](#security). A provider can reach what you approved, no more, unless you granted it a
way out (a socket, or writing something that runs later).

A parser is given no way to run or write anything and shares nothing with its caller, which is what
makes one from a stranger reasonable to use. What it returns is still data from that stranger: a
provider should not paste it into a shell command. It can also waste time and memory until its host
is killed, and with it the provider using it.

This rests on the allow-list and the separate Lua state, and, for a parser running by itself, on the
[sandbox](#the-sandbox) as well. It has been checked by a few tests, not audited.

The daemon never runs as root, and a mod cannot gain rights: `sudo` and `pkexec` do not work in the
sandbox. Something that needs root is not a mod's job; do it from a dialog or a script of your own,
outside spot's mods.

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

`from`, `call` and the command line do not know which of the two is behind a name. Names are reserved
from the start: one owner per name, and a backend cannot take the name of an installed mod. How
backends are approved is decided before 0.12.

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

**Security mechanisms**, tested in throwaway programs on Linux 7.3:

| Question | Result |
|----------|--------|
| Resolve, check, then open, while a folder is swapped for a link | read the outside file 488 times of 20,000 |
| `openat2` with `RESOLVE_NO_SYMLINKS` | 0 of 20,000; a hard link still reads the file it shares |
| A file left open without close-on-exec | reached the program; `close_range` stopped that |
| A pipe as the channel | the program wrote a forged message; not with a socket pair, nor with `PR_SET_DUMPABLE` 0 |
| Reading the host's environment | possible, until `PR_SET_DUMPABLE` 0 |
| Stopping: process group / subreaper / `cgroup.kill` / the last two together | left detached programs / only `systemd` jobs / a program that moved itself / only `systemd` jobs |
| "Links may go anywhere inside `/sys`" | permission for `/sys/class/power_supply` reached all of `/sys` |

**The sandbox**, a throwaway harness on Linux 7.3 (Landlock ABI 11), same probes with and without:

| Attempt | Sandboxed |
|---------|-----------|
| Read or write outside the grants; follow a symlink out | refused |
| Link or move a file from outside, or a read-only one, into a writable folder | refused (linking inside it works) |
| Read through a hard link already inside a granted folder | **works** |
| Read or signal the parent process | refused |
| Connect to a socket file or an abstract socket | refused |
| TCP connection; a UDP datagram arriving | refused |
| Start a job through `systemd` | refused |
| `sudo` | refused |
| A program not granted, through the dynamic loader | **starts**, still confined |
| `ss`, `node`, a battery read with only that device's folder | work |
| `hyprctl`, `systemctl --user` without their socket | fail |
| The same with the socket granted | work, **and can then run anything outside the sandbox** |

Starting through the harness cost about 1.5 ms. `/etc` was granted whole in these runs, which exposed
the UFW rules: hence S37.

**What Landlock alone does not stop**, found by a later audit and confirmed on the same machine,
then tried with a seccomp filter added:

| Attempt, with no permission for it | Landlock | + filter |
|---|---|---|
| Multipath TCP to an ordinary TCP server | **connects** | refused |
| ICMP ping socket, VM socket, netlink socket | **opens** | refused |
| `chmod` or set an attribute on a file outside the grants | **works** | refused |
| Create a user namespace, set up `io_uring` | **works** | refused |
| `stat` a file outside | works | works |
| Start an unlisted program through the loader, with programs granted one by one | refused (a binary copied into a writable folder still starts) | |
| Read `/proc/net` from a child, granted to the parent | refused | |

With the filter, `sh`, `python3`, `node`, `java`, `cp`, `mv`, `sed -i`, `hyprctl` and `systemctl --user`
(with their sockets) still worked; `cp -p` and `install -m` failed.

**A second round**, on the filter as first tried and then extended, netlink `diag` granted:

| Attempt | First filter | Extended |
|---|---|---|
| SysV shared memory, semaphores, message queue | **works** | refused |
| Add to or read a kernel keyring | **works** | refused |
| `memfd_create`; run a program with `execveat` | **works** | refused |
| netlink `route` and `audit` (only `diag` granted) | **works** | refused |
| `userfaultfd`, `pidfd_open`, `perf_event_open` | **works** | refused |
| POSIX message queue | refused (Landlock) | refused |
| UDP to a port that was not granted | refused (Landlock) | refused |

With the extended filter, `sh` pipelines, `python3` and `node` and Go starting programs, `java`,
`git`, `ss`, `hyprctl`, `systemctl --user`, `wpctl`, `pactl`, `bluetoothctl`, `powerprofilesctl` and
`brightnessctl` still worked. The re-exec was checked from 200 threads, and the second stage started on
its own refused to go on.

**The base grant**, 29 programs (`ss`, `hyprctl`, `systemctl`, `wpctl`, `bluetoothctl`, `pacman`, `git` and
others), each with the sockets or folders its permissions would add:

| Tried | Result |
|---|---|
| The base above | 27 gave the same result as unsandboxed; `ip` lacked a netlink permission, `sensors` its config file |
| A Go host with gopher-lua, only its own file granted | runs Lua and executes itself again; 4.2 MB without GTK |
| A listed shell starting `/usr/bin/curl`, directly or through the loader | refused |
| A listed shell starting a program in `/usr/lib` through the loader | **starts**, still confined |
| A file or program granted by name, then replaced by rename | **refused** from then on; through a folder grant it keeps working |

**Network rules**, one port granted, against servers on the same machine:

| Attempt | Result |
|---|---|
| TCP to the granted port | works |
| TCP to another port, also through an IPv4-mapped IPv6 address | refused |
| TCP Fast Open to another port (`sendto`, `sendmsg`) | refused |
| `bind` and `listen` | refused |
| `listen` without `bind` | **listens on every interface**; a connection to the machine's network address was accepted. Refused once the filter denies `listen` |
| UDP to another port, also from a socket connected to the granted one | refused |

**Name lookups and HTTPS** from `curl`, `getent`, Python and a Go program, with `resolv.conf` and
the certificate folders added to the base grant: all worked, and ports 80 and 8443 of the same server
were refused. Without the certificate folders HTTPS failed.

**Still to test:** older kernels (if the minimum is to be lowered), other distributions, large DNS answers and `.local` names, the real host's start
time, and enforcement of cgroup limits.

### First version

In: the mod host with its time and memory limits, loading and enabling, the `parser` and `provider` types, the parser contract and lockdown, `spot.use`, `spot parse`, `spot mods check`, mod health and notifications, permissions with reasons, the scanner and the strict form, approval and the audit log, the sandbox, the socket checks, `run` / `parse` in dialogs, `value` / `publish` / `refresh` / `on`, the helpers above except `watch_file`,
`get` / `watch` / `call` / `mods`, and `from` / `call` in dialogs.

Later: `watch_file`, built-in system events (power, network), automatic reload on save.

Not in 0.9: **launch hooks**, which need their own design. Core data such as the app index and launch
history needs a permission of its own and counts as private data in warnings. "Make executable and
run" from [#13](https://github.com/KopyTKG/spot/issues/13) is not offered as a mod request: a
confirmation in a dialog does not protect a request that `spot call` can reach.

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
