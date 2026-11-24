#!/bin/bash
# windows.sh — a window switcher: every open window as "app — title", type to filter,
# Enter focuses it. Works on Hyprland and sway.
#
#   windows.sh          the picker
#   windows.sh list     print "label<TAB>id" rows and exit
#
#   Hyprland:  hl.bind("ALT + TAB", hl.dsp.exec_cmd("/path/to/windows.sh"))
#   sway:      bindsym Mod1+Tab exec /path/to/windows.sh
#
# Needs jq, and hyprctl or swaymsg.
here=$(dirname "$(readlink -f "$0")")

if [[ -n ${HYPRLAND_INSTANCE_SIGNATURE:-} ]] && command -v hyprctl >/dev/null; then
  compositor=hyprland
  list() { hyprctl clients -j | jq -r '.[] | select(.mapped) | "\(.class)  —  \(.title)\t\(.address)"'; }
  focus() {                                              # classic dispatcher first, then the Lua config's
    [[ $(hyprctl dispatch focuswindow "address:$1") == ok ]] ||
      hyprctl dispatch "hl.dsp.focus({ window = \"address:$1\" })" >/dev/null
  }
elif command -v swaymsg >/dev/null; then
  compositor=sway
  list() {
    swaymsg -t get_tree | jq -r '[recurse(.nodes[]?, .floating_nodes[]?)
        | select((.pid // 0) > 0 and (.name != null) and ((.nodes | length) == 0))]
        | .[] | "\(.app_id // .window_properties.class // "?")  —  \(.name)\t\(.id)"'
  }
  focus() { swaymsg "[con_id=$1] focus" >/dev/null; }
else
  echo "windows.sh: needs Hyprland (hyprctl) or sway (swaymsg)" >&2; exit 1
fi

[[ ${1:-} == list ]] && { list; exit 0; }

windows=$(list)
[[ -n $windows ]] || exit 0
choice=$(cut -f1 <<<"$windows" | SPOT_TITLE="Windows ($compositor)" spot "$here/windows.toml") || exit 0
[[ -n $choice ]] || exit 0
id=$(PICK=$choice awk -F'\t' '$1 == ENVIRON["PICK"] { print $2; exit }' <<<"$windows")   # first window with that label
[[ -n $id ]] && focus "$id"
