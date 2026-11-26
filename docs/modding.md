# Modding: daemon mods (draft)

> **Draft for review. Nothing described here exists yet.** It proposes the mod API for 0.9
> ([#16](https://github.com/KopyTKG/spot/issues/16)); names and behaviour are all open to change.
> Questions to answer are [at the end](#open-questions).

A **daemon mod** is a Lua file the spot daemon runs. It loads and parses data once, keeps it, and
hands it to anything that asks: dialogs, a bar, scripts. It replaces the shell script that is run
again for every consumer.

```
            ┌─────────────── spot daemon ───────────────┐
 ss, ufw,   │  mod "sockets" ──publishes──► sockets.*   │──► dialogs   (from = "sockets.table")
 files  ───►│  mod "idle"    ──publishes──► idle.*      │──► bars      (spot watch sockets.count)
            │       ▲                                   │──► scripts   (spot get / spot call)
            └───────┼───────────────────────────────────┘
                    └──────── requests: spot call idle.set false
```

**UI mods** (behaviour inside one dialog) are a separate layer, planned for 0.12 and not covered
here.

## Types of mod

| Type | Does | May | Runs |
|------|------|-----|------|
| `parser` | turns text into data | compute only: no commands, files, timers or publishing | inside whoever uses it |
| `provider` | gets data (usually with a parser), publishes it, answers requests | everything in [the API](#the-api) | in the daemon, as long as it does |
| `ui` | behaviour inside one dialog | (0.12, not designed here) | while the dialog is open |

A mod says what it is in `mod.toml` next to its `init.lua`, so the daemon knows what it may do
before running any of it:

```toml
type = "parser"          # "provider" is the default
uses = []                # parsers this mod needs
```

Splitting them means the parsing of `ss`, `ufw status` or `~/.ssh/config` is written once, shared
by every tool that needs it, and testable without running anything.

## A complete example

A parser, `~/.config/spot/mods/ss/init.lua`. It returns its functions:

```lua
local M = {}

-- the output of `ss -H -tuna`
function M.sockets(text)
  local rows = {}
  for line in text:gmatch("[^\n]+") do
    local proto, state, _, _, addr, peer = line:match("(%S+)%s+(%S+)%s+(%S+)%s+(%S+)%s+(%S+)%s+(%S+)")
    if proto then
      rows[#rows + 1] = { label = ("%-4s %-9s %s  %s"):format(proto, state, addr, peer), key = addr }
    end
  end
  return rows
end

return M
```

A provider that uses it, `~/.config/spot/mods/sockets/init.lua` (with `uses = ["ss"]` in its
`mod.toml`):

```lua
local ss = spot.use("ss")

local function sockets()
  local r = spot.run({ "ss", "-H", "-tuna" })
  if r.status ~= 0 then error(r.stderr) end
  return ss.sockets(r.stdout)
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

`spot.use(name)` returns the table the parser's `init.lua` returned. The code is loaded into the
caller's own Lua state: using a parser is an ordinary function call, with no waiting on another mod
and nothing shared between callers.

A parser gets `string`, `table`, `math`, `spot.json`, `spot.log` and `spot.use` (for other
parsers), and nothing else. Its functions take text, and optionally a table of options, and return
the same values a provider publishes.

A parser also works **without writing a provider**:

```sh
ss -H -tuna | spot parse ss.sockets          # try it, or use it from a script
spot parse ss.sockets < saved-output.txt     # check it against a saved sample
```

```toml
# a dialog that only needs to run a command and show the result
[[items]]
type  = "list"
id    = "socket"
run   = "ss -H -tuna"
parse = "ss.sockets"
```

So a tool can start as a command and a parser, and become a provider only when it needs to keep
data, refresh it or answer requests. Parsers named by a dialog or `spot parse` are found in the
mods folder and need no enabling: they cannot do anything but compute.

## Files and enabling

| | |
|---|---|
| A mod | a folder `<name>/` with `init.lua` and optionally `mod.toml`; `require` finds other files in that folder only |
| Where | `$XDG_CONFIG_HOME/spot/mods/`, or `mods_dir` in `daemon.toml` |
| Name | `[a-z][a-z0-9_-]*`; it prefixes everything the mod publishes |
| Enabled | a provider: listed in `mods` in `daemon.toml`, otherwise it never runs. A parser: when something uses it |
| Settings | the `[mod.<name>]` table of `daemon.toml`, visible to the mod as `spot.config` |

## The API

This is what a **provider** gets. A mod sees one global, `spot`, and the Lua 5.1 `string`, `table`, `math` and `coroutine` libraries.
`io` and `os` are left out: a blocking call there could not be timed out. The helpers below take
their place.

### Publishing

| Call | Meaning |
|------|---------|
| `spot.value(name, options, fn)` | A value computed by `fn` **when somebody wants it**. `ttl` (ms): reuse the result that long. `every` (ms): recompute on this interval, but only while somebody is watching |
| `spot.publish(name, data)` | Set a value now, for mods that learn of changes themselves |
| `spot.refresh(name)` | Recompute a `value` now and tell watchers |

A value is a string, a number, a boolean, a table, or a list of rows. A **row** is
`{ label, key, state, comment, icon }`; only `label` is required. `state` (a boolean) makes it a
switch row.

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
| `spot parse <mod.function>` | Run a parser on standard input |

`--json` prints `get` and `watch` as one JSON value per line, which is what a bar wants.
`get`, `watch`, `call`, `parse` and `mods` become reserved words: a config of that name needs a path.
`watch` reconnects when the daemon is restarted, for instance after an upgrade.

## When things go wrong

| Case | What happens |
|------|--------------|
| A mod fails to load | It is marked failing; the others and all dialogs work |
| A `value` function errors | Readers get the last good result and the error is logged; `get` exits non-zero if there is none |
| Three errors in a row | The value stops being recomputed until the mod is reloaded or asked again after a pause |
| A function runs longer than 5 s | It is interrupted; `spot.run` kills the process group |
| A `from` names a mod that is not running | The dialog shows the error under the item |

**Isolation.** Each mod has its own Lua state on its own goroutine, with a queue of work. A slow
mod delays only itself, and mods cannot read each other's variables; they share only what they
publish.

**Limits we cannot promise.** A mod can use unlimited memory, and the daemon would go down with it.

## Trust

A provider runs **with all your rights**: it can run any program and write any file you can. It is not a
sandbox and does not pretend to be. Enable only mods you have read, exactly as with a shell script.
A parser is given no way to run or write anything, so sharing one is low-risk, though a bad one can
still waste time or memory. The daemon never runs as root; privileged actions go through your own helper (`sudo`, `pkexec`).

## Building it

- A new package `internal/mods`, without GTK, hosted by `internal/daemon`.
- Lua: [gopher-lua](https://github.com/yuin/gopher-lua), written in Go, so no new C dependency.
  Its support for interrupting a running script and the cost of that **still need checking**.
- The socket protocol gains `get`, `watch` and `call`. `watch` answers many times on one
  connection, where today every request has one answer.
- `daemon.toml` gains `mods`, `mods_dir` and `[mod.*]`. Older versions of spot reject unknown keys
  there and fall back to their defaults with a warning.

### One model for mods and backends

A long-running **external backend** (0.12) should look the same to everything that reads data: it
publishes values and answers requests, only over JSON lines instead of Lua calls.

```json
{"publish": "table", "data": [{"label": "tcp LISTEN", "key": "0.0.0.0:22"}]}
{"request": 7, "name": "set", "args": ["false"]}
{"reply": 7, "ok": true, "data": false}
```

`from`, `call` and the command line would not know which of the two is behind a name.

### First version

In: loading and enabling, the `parser` and `provider` types, `spot.use`, `spot parse`, `run` / `parse` in dialogs, `value` / `publish` / `on`, `run`, `read`, `every`, `json`, `log`,
`get` / `watch` / `call` / `mods`, and `from` / `call` in dialogs.

Later: `watch_file`, built-in system events (power, network), automatic reload on save, UI mods.

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
8. **Types.** Are parser and provider enough, or is a third kind needed, such as one that only
   performs actions?
9. **For whoever asked for modding:** what would you build first, and can this API do it?
