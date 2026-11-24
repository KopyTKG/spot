#!/bin/bash
# power-menu.sh — lock / suspend / log out / reboot / shut down, as a tile menu.
#
# spot draws the tiles and prints the chosen value; this script maps it to a command.
# Reboot and shut down ask first (confirm.toml with a title), so a mis-click is safe.
#
#   Hyprland:  hl.bind("SUPER + ESCAPE", hl.dsp.exec_cmd("/path/to/power-menu.sh"))
#   sway:      bindsym $mod+Escape exec /path/to/power-menu.sh
#
# Needs systemd (loginctl, systemctl). "Lock" uses loginctl lock-session, which asks
# whatever locker you run (hyprlock, swaylock via swayidle, ...) to lock.
here=$(dirname "$(readlink -f "$0")")

choice=$(spot "$here/power-menu.toml") || exit 0       # Esc / click outside: nothing happens

ask() {                                                  # ask <title>: succeed only on "yes"
  [[ $(SPOT_TITLE="$1" SPOT_DEFAULT=no spot "$here/../confirm.toml") == yes ]]
}

case $choice in
  lock)     loginctl lock-session ;;
  suspend)  systemctl suspend ;;
  logout)   loginctl terminate-session "${XDG_SESSION_ID:-self}" ;;
  reboot)   ask "Restart now?"  && systemctl reboot ;;
  poweroff) ask "Shut down now?" && systemctl poweroff ;;
esac
