# Example mods

- [`ss`](ss/): a **parser**. Turns the output of `ss -H -tuna` into records.
- [`sockets`](sockets/): a **provider**. Keeps the machine's sockets for a table and a bar.

Mods are in development. The design is in [docs/modding.md](../../docs/modding.md).

## Install and approve

```sh
mkdir -p ~/.config/spot/mods
cp -r examples/mods/ss examples/mods/sockets ~/.config/spot/mods/
echo 'mods = ["sockets"]' >> ~/.config/spot/mods.toml

spot mods check sockets      # what the scanner points at
spot mods allow sockets      # read it, then type what you accept
```

The parser it uses is approved together with it.

## Use

```sh
spot get sockets.table                      # rows: label, key
spot watch sockets.count                    # now and on every change
spot mods                                   # approved, running, failing
spot mods audit sockets                     # what it ran
spot ~/.config/spot/mods/sockets/sockets.toml   # a searchable table; Enter copies the address
```

For waybar:

```jsonc
"custom/sockets": {
  "exec": "spot watch --waybar sockets.count",
  "return-type": "json",
  "on-click": "spot ~/.config/spot/mods/sockets/sockets.toml"
}
```

## Trying a build beside your installed spot

From a checkout, `just build`, then use `just dev` where the commands above say `spot`:

```sh
just dev mods allow sockets
just dev get sockets.count
just dev ~/.config/spot/mods/sockets/sockets.toml
just dev --quit              # stop the second daemon
```

It has its own daemon and socket; your installed `spot` keeps running and does not read `mods.toml`.
Run `just dev` from the checkout folder.

## Compared with a shell script

The sockets mod replaces a script of about 130 lines that did the same with `ss`, `awk` and `sort`.

| | Script | Mod |
|---|---|---|
| Parsing | `awk`, inside the script | a parser others can use, with a declared shape |
| The dialog | built by the script, which feeds it | `from = "sockets.table"`, no script |
| Bar | polled: a new process each time | `watch`: pushed when it changes |
| Rights | everything you can do | run `ss`, read the socket tables; nothing else |
| Failure | an empty menu or bar | you are told, and the last good value is kept |
| **Process names** (`ss -p`) | yes | **no**: a mod may not read other processes' files |
| **Root mode** | yes, through `sudo` | **no**: a mod cannot gain rights |
