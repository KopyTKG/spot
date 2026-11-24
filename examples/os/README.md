# Tying spot to your system

Small, real scripts that connect spot to things on a Linux desktop: power, the clipboard,
emoji, sockets, passwords, windows. Each is a shell script plus the spot config it uses, in
this folder, so they run from here as they are:

```sh
./examples/os/power-menu.sh
```

The scripts do the system work; spot is the window. The guide that explains the patterns
is [`docs/os-integration.md`](../../docs/os-integration.md).

| Script | What it does | Needs | Typical key |
|--------|--------------|-------|-------------|
| [`power-menu.sh`](power-menu.sh) + [`.toml`](power-menu.toml) | Lock, suspend, log out, restart, shut down as tiles; restart and shut down ask first | systemd | Super+Esc |
| [`clipboard.sh`](clipboard.sh) + [`.toml`](clipboard.toml) | Clipboard history; Enter puts an entry back | `cliphist`, `wl-clipboard` | Super+Ctrl+V |
| [`emoji.sh`](emoji.sh) + [`.toml`](emoji.toml) | Emoji picker: a collapsible list, one group per category, searchable with chat shortcodes | `unicode-emoji` (+ `python-emoji`), `wl-clipboard` | Super+Ctrl+E |
| [`sockets.sh`](sockets.sh) + [`.toml`](sockets.toml) | `ss` as a searchable table, with a header that lines up; Enter copies the address | `ss`, `wl-clipboard` | |
| [`windows.sh`](windows.sh) + [`.toml`](windows.toml) | Window switcher for Hyprland and sway | `jq`, `hyprctl` or `swaymsg` | Alt+Tab |
| [`askpass.sh`](askpass.sh) + [`.toml`](askpass.toml), [`root.sh`](root.sh) | A password dialog for `sudo -A` and `ssh`, that says *why* it is asking | `sudo` | |

Every config here shares [`../theme.css`](../theme.css). Each script locates its config through
its own path, so you can copy the folder anywhere. Bind a script to a key in your compositor
(see the guide), and start the daemon at login so the first press is instant
([`docs/daemon.md`](../../docs/daemon.md)).
