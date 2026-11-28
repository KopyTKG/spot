# Examples

Every file here parses against the current spot (a test checks it), and most are meant to
be run as they are: `spot examples/<name>.toml`. They share one stylesheet,
[`theme.css`](theme.css). For **every key in one file** start with
[`config-reference.toml`](config-reference.toml).

## Every component

[`components/`](components/) is one menu that opens a demo of each component, with pretend settings:
`cd components && spot demo.toml`.

## Forms and prompts

| File | Shows |
|------|-------|
| [`form.toml`](form.toml) | A field and a dropdown with an `exec` submit |
| [`ssh.toml`](ssh.toml) + [`ssh.css`](ssh.css) | A form of fields that opens `ssh` in a terminal (`exec` + `spawn`) and has its own stylesheet |
| [`password.toml`](password.toml) | A password prompt printed to stdout |
| [`confirm.toml`](confirm.toml) | Yes / No tiles; `SPOT_TITLE`, `SPOT_DEFAULT` |
| [`widgets.toml`](widgets.toml) | `text`, `toggle`, `scale`, a multi-select list, validation and JSON output |
| [`settings.toml`](settings.toml) | live `switch`, `segmented`, `scale` and `button` that apply at once |
| [`switches.toml`](switches.toml) | a searchable list mixing switches and ordinary choices |

## Menus and lists

| File | Shows |
|------|-------|
| [`menu.toml`](menu.toml) | A dmenu-style searchable list fed from stdin |
| [`power.toml`](power.toml) | A two-section tile menu using `{_active}` |
| [`help.toml`](help.toml) | A **collapsible** list: a help screen whose sections fold and unfold, searchable inside folded groups |
| [`back.sh`](back.sh) | A menu → submenu → dialog flow with **Back**, and the same dialog opened directly with none |

## Launcher

| File | Shows |
|------|-------|
| [`launcher.toml`](launcher.toml) | An **application launcher** with prefix sources: `=` calculator, `.` files, `@` web search, `>` run |
| [`run.toml`](run.toml) | A **run bar**: just a search box (default source, `hide_empty`, positioned from the top) |
| [`sources/`](sources/) | The four commands the launcher examples call: `calc.sh`, `files.sh`, `web.sh`, `run.sh`. Each header shows the `[[sources]]` entry that uses it |

The guide for all of this is [`docs/launcher.md`](../docs/launcher.md).

## Tying spot to the system

Runnable scripts for a real desktop (power menu, clipboard history, emoji picker, sockets table,
window switcher, a password dialog for `sudo` / `ssh`), each with its config. See
[`os/`](os/) and the guide, [`docs/os-integration.md`](../docs/os-integration.md).

| Folder | Shows |
|--------|-------|
| [`os/`](os/) | Seven scripts that connect spot to systemd, the clipboard, the compositor, `ss` and `sudo` |

## Daemon and styling

| File | Shows |
|------|-------|
| [`daemon.toml`](daemon.toml) | The daemon's own settings (socket, log, timing), every key with its default; not a dialog |
| [`theme.css`](theme.css) | A complete theme: every widget, validation and error states, the launcher rows |
| [`demo/`](demo/) | A guided tour: `./examples/demo/demo.sh` walks through a form with every input widget, tiles, a stdin list and a failing `exec` |
