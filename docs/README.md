# spot documentation

| Page | What's in it |
|------|--------------|
| [`toml.md`](toml.md) | **Reference** for every TOML setting: window and placement, `[opacity]`, `[back]`, each widget type, collapsible lists, `apps`, `[apps]`, `[[sources]]`, validation, `[submit]`, environment overrides |
| [`launcher.md`](launcher.md) | **Guide** to building a launcher, a run bar or any search-and-run picker: apps, sources, recipes, keybinds, placement, styling, troubleshooting |
| [`css.md`](css.md) | **Styling**: the class tree, every `.spot-*` class, states, per-widget notes, a starter stylesheet |
| [`daemon.md`](daemon.md) | **The daemon**: on demand, systemd user service, autostart, `daemon.toml`, control flags, troubleshooting |
| [`roadmap.md`](roadmap.md) | Where spot is going, and what it is not |

Also in the repository:

- [`../CHANGELOG.md`](../CHANGELOG.md): what changed in each release
- [`../examples/`](../examples/): runnable configs, indexed by what they show ([`README`](../examples/README.md)); [`config-reference.toml`](../examples/config-reference.toml) lists **every key** in one file, [`theme.css`](../examples/theme.css) is a complete theme, [`sources/`](../examples/sources/) are the launcher's source scripts
- [`../packaging/systemd/spot.service`](../packaging/systemd/spot.service): the systemd user unit
