#!/usr/bin/env bash
# Back navigation, end to end: a main menu, a Settings submenu, and a Display
# dialog that can be opened from that menu *or* directly.
#
#   ./back.sh             Main menu -> Settings -> Display. Back goes up one level.
#   ./back.sh display     Display on its own (think: a keybind): no Back.
#
# Two ways to ask for Back; both make Back exit with status 4:
#   spot --back cfg       show Back; the script handles status 4 itself (settings, below)
#   SPOT_BACK='cmd' spot  show Back AND run cmd when it is used (display, below).
#                         It is an environment variable, so it reaches every dialog
#                         a child script opens, with no change to the child.
#
# Needs spot >= 0.4 and a Wayland session. Esc closes everything, as always.
cd "$(dirname "$0")" || exit 1
self=$(readlink -f "$0")

menu() { SPOT_TITLE=$1 spot "${@:2}" menu.toml; }   # items on stdin; prints the choice

main_menu() {
  # --chain: Settings follows, so keep the window up for it (no close and reopen)
  choice=$(printf '%s\n' Settings Quit | menu Main --chain) || return
  [[ $choice == Settings ]] && settings
}

settings() {
  choice=$(printf '%s\n' Display Network | menu Settings --back); rc=$?
  (( rc == 4 )) && { main_menu; return; }            # Back: up one level
  # Opened from this menu, so Display's own dialog gets a Back that returns here.
  [[ $choice == Display ]] && SPOT_BACK="$self settings" "$self" display
}

display() {
  # Written without knowing who called it: Back appears only if SPOT_BACK is set.
  choice=$(printf '%s\n' 1920x1080 2560x1440 3840x2160 | menu Display) || return
  echo "picked: $choice"
}

case ${1:-main} in
  main)     main_menu ;;
  settings) settings ;;
  display)  display ;;
  *)        echo "usage: $0 [main|settings|display]" >&2; exit 2 ;;
esac
