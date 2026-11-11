# spot documentation

| Page | What's in it |
|------|--------------|
| [`toml.md`](toml.md) | Every TOML setting: window, `[opacity]`, `[back]`, each widget type, validation, `[submit]`, environment overrides |
| [`daemon.md`](daemon.md) | The daemon: on demand, systemd user service, autostart, `daemon.toml`, control flags, troubleshooting |
| [`css.md`](css.md) | Styling: the class tree, states, per-widget notes, a starter stylesheet |

Runnable material lives next to the code:

- [`../examples/config-reference.toml`](../examples/config-reference.toml): every key in one commented file
- [`../examples/daemon.toml`](../examples/daemon.toml): every daemon setting, with defaults
- [`../packaging/systemd/spot.service`](../packaging/systemd/spot.service): the systemd user unit
- [`../examples/demo/`](../examples/demo/): a guided tour (`./demo.sh`)
- [`../examples/theme.css`](../examples/theme.css): a complete theme
