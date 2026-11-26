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
| `provider` | gets data (usually with a parser), publishes it, answers requests | everything in [the API](#the-api) | in its own mod host, as long as the daemon runs |

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

[returns]
type   = "list"
fields = { label = "string", key = "string", comment = "string?" }
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

A provider that uses it, `~/.config/spot/mods/sockets/init.lua` (with `uses = ["ss"]` in its
`mod.toml`):

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

Enable the provider in `~/.config/spot/daemon.toml`. The parser it uses comes with it:

```toml
mods = ["sockets"]
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

**One function.** `init.lua` returns it and nothing else:

```lua
return function(input, options) ... return output end
```

| | |
|---|---|
| `input` | a string: the text to parse |
| `options` | a table of plain data, empty when none are given |
| `output` | plain data matching `[returns]` |
| Failure | `error("what was wrong")`, the same for every parser |

**Plain data** is strings, numbers, booleans and tables of those. Functions, threads and tables that
contain themselves or nest more than 32 deep are refused.

**A declared shape.** `mod.toml` of a parser must have `api`, `version`, `description`, `input` and
`[returns]`:

| In `[returns]` | |
|---|---|
| `type` | `"list"`, `"map"`, `"string"`, `"number"` or `"boolean"` |
| `fields` | for a list, what each item has; for a map, what it has. Each is a type, with `?` when it may be missing |

Only what is declared is checked, as deep as the author chooses to promise: further fields and
whatever is nested in them pass through. Output that does not match is an error, so a user of the
parser can rely on the declaration without reading the code. A change that breaks it needs a new
major `version`.

### Calling it

Every parser is called the same way, by the name of its folder:

```lua
local parse = spot.use("ss")                 -- in a mod
local rows  = parse(text, { listening = true })
```

```sh
ss -H -tuna | spot parse ss                  # in a shell, prints JSON
```

```toml
# in a dialog: run a command, show what the parser makes of it
[[items]]
type  = "list"
id    = "socket"
run   = "ss -H -tuna"
parse = "ss"                                  # needs a list with a label
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

### Checking a shared parser

A parser can carry a sample: `example/input.txt` and the `example/output.json` it should give.
`spot mods check ss` runs it, checks the contract and the shape, and says so when there is no sample.
It shows that the parser does what it claims on that input, not that it is correct on all inputs.

## Files and enabling

| | |
|---|---|
| A mod | a folder `<name>/` with `init.lua` and optionally `mod.toml`; `spot.require` loads other files from that folder only |
| Where | `$XDG_CONFIG_HOME/spot/mods/`, or `mods_dir` in `daemon.toml` |
| Name | `[a-z][a-z0-9_-]*`; it prefixes everything the mod publishes |
| Enabled | a provider: listed in `mods` in `daemon.toml`, otherwise it never runs. A parser: when something uses it |
| Settings | the `[mod.<name>]` table of `daemon.toml`, visible to the mod as `spot.config` |

## The API

This is what a **provider** gets: one global, `spot`, and the Lua environment below.

### The Lua environment

Lua 5.1. Every mod gets an **allow-list**; everything else is removed before any mod code runs:

`assert` `error` `getmetatable` `ipairs` `next` `pairs` `pcall` `rawequal` `rawget` `rawset`
`select` `setmetatable` `tonumber` `tostring` `type` `unpack` `xpcall`, and the libraries `string`,
`table`, `math`, `coroutine`.

- `io`, `os`, `package` and `debug` are not loaded. A blocking call there could not be timed out;
  the helpers below take their place.
- The Lua library's base functions include some that read files, `dofile` and `loadfile`, even with
  `io` left out. An allow-list keeps them out, and also whatever a later version of the library adds.
- Code is shared with `spot.use` (parsers) and `spot.require` (other files of the same mod's
  folder), both done by spot, which only reads from the mods folder.
- A provider gets the same list plus its `spot`; parsers also lose `math.random` and
  `math.randomseed`.

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
| `spot.run(argv, options)` | `{ status, stdout, stderr }`. `argv` is a list, **no shell**. Options: `timeout` (ms, default 5000), `stdin`, `env`, `dir` |
| `spot.sh(command, options)` | The same through `sh -c`, for pipes. Values are not quoted for you |
| `spot.read(path)`, `spot.write(path, text)` | Whole files; `nil, message` on failure |
| `spot.watch_file(path, fn)` | `fn` when it changes |
| `spot.every(ms, fn)`, `spot.after(ms, fn)` | Timers |
| `spot.json.encode(v)`, `spot.json.decode(s)` | JSON |
| `spot.env(name)`, `spot.time()` | An environment variable; seconds since 1970 |
| `spot.log(message)` | To the daemon log, with the mod's name |

Mods run in the **daemon's** environment, not that of whoever asks.

### Limits

| Limit | Default | |
|-------|---------|---|
| Time of one function | 5 s | then it is interrupted, or the host is killed |
| Memory of a mod host | 256 MB | a data limit (`RLIMIT_DATA`); `memory` in `[mod.<name>]` changes it |
| Rows in a value | 10,000 | larger values are refused |

Programs a mod starts get their normal memory limit back before they run: some, such as `node`, do
not start under the host's. Their memory is therefore **not limited**.

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
| `spot parse <parser>` | Run a parser on standard input, print JSON |
| `spot mods check [name]` | Check a mod's contract and sample |

`--json` prints `get` and `watch` as one JSON value per line, which is what a bar wants.
`get`, `watch`, `call`, `parse` and `mods` become reserved words: a config of that name needs a path.
`watch` reconnects when the daemon is restarted, for instance after an upgrade.

## When things go wrong

| Case | What happens |
|------|--------------|
| A parser breaks its contract (more than a function, no `[returns]`, wrong shape) | The call fails and names what did not match |
| A mod fails to load | It is marked failing; the others and all dialogs work |
| A `value` function errors | Readers get the last good result and the error is logged; `get` exits non-zero if there is none |
| Three errors in a row | The value stops being recomputed until the mod is reloaded or asked again after a pause |
| A function runs longer than 5 s | It is interrupted inside the host; `spot.run` kills the process group |
| The host does not answer in time (stuck where it cannot be interrupted) | The daemon kills the host, and the commands it started, and starts a new one |
| A mod uses more than its memory limit | Its host dies; the daemon starts a new one |
| A published value has more than 10,000 rows | It is refused with an error naming the value |
| A host dies three times in a row | The mod is disabled until `spot mods reload`; the error is shown by `spot mods` |
| A `from` names a mod that is not running | The dialog shows the error under the item |

A restarted mod starts empty. The daemon keeps the last values it published, marked as old, until
new ones arrive.

**Why a separate process.** Two things cannot be contained inside one program, as a test of the Lua
library showed:

- A slow string pattern runs in Go code that a time limit cannot interrupt: a short pattern took
  1.5 s on 200 characters and 23 s on 400.
- Running out of memory is fatal and cannot be caught: one oversized `string.rep`, or `s = s .. s`
  in a loop, ended the process.

Mods are isolated from each other in the same way: separate processes, sharing only what they
publish. Parsers a mod uses run in its process, so a bad parser takes that mod's host down with it.

## Trust

A provider runs **with all your rights**: it can run any program and write any file you can. The
separate process protects the daemon from a broken mod, not you from a malicious one. Enable only
mods you have read, exactly as with a shell script.

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

In: the mod host with its time and memory limits, loading and enabling, the `parser` and `provider` types, the parser contract and lockdown, `spot.use`, `spot parse`, `spot mods check`, `run` / `parse` in dialogs, `value` / `publish` / `on`, `spot.require`, `run`, `read`, `every`, `json`, `log`,
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
5. **`io` and `os`.** Is leaving them out acceptable, or will mods need libraries that expect them?
6. **Commands as lists.** `spot.run` takes a list to avoid quoting bugs. Too strict for how you
   write scripts?
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
