# Component demos

One menu that opens a demo of every component. The settings are pretend ones
(files under `$XDG_RUNTIME_DIR/spot-demo`): nothing on your system changes, except
that *A form* sends a notification and *A command in a terminal* opens one.

```sh
cd examples/components          # or /usr/share/doc/spot/examples/components
spot demo.toml
```

Run it from this folder: the commands in the demos are relative to it. Copy the
folder to start your own.

| File | Shows |
|------|-------|
| `toggles.toml` | tiles that switch, a plain tile, an unreadable state, per-tile classes, meters, a button |
| `controls.toml` | `switch` in its three looks, `segmented`, live `scale`, `separator` |
| `lists.toml` | switch rows in a list, groups that fold |
| `form.toml` | `field` with validation, `select`, multi `list`, `toggle`, `scale`, `text` |
| `demo.toml`, `menu/` | menu pages, a page as a grid, Back, search into deeper pages, `include` |

Needs spot 0.10.1 or newer; the grid page 0.10.2. Not shown here: the app launcher
([`../launcher.toml`](../launcher.toml)), passwords ([`../password.toml`](../password.toml)) and
[mods](../mods/).
