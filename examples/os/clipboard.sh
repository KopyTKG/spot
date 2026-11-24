#!/bin/bash
# clipboard.sh — clipboard history picker: a spot list of what you copied, newest first.
# Enter puts the chosen entry back on the clipboard (paste it with Ctrl+V / Shift+Insert).
#
# History comes from cliphist; start its watchers once per session:
#   wl-paste --type text  --watch cliphist store &
#   wl-paste --type image --watch cliphist store &        # Hyprland: exec-once = ...
#
#   Hyprland:  hl.bind("SUPER + CTRL + V", hl.dsp.exec_cmd("/path/to/clipboard.sh"))
#
# Needs cliphist and wl-clipboard.
here=$(dirname "$(readlink -f "$0")")

command -v cliphist >/dev/null || { notify-send -u critical "clipboard.sh" "cliphist is not installed"; exit 1; }

history=$(cliphist list)                      # one entry per line: ID<TAB>preview, newest first
[[ -n $history ]] || { notify-send -u low "Clipboard" "Nothing copied yet"; exit 0; }

# spot shows the previews; the chosen preview is looked up again to find its ID.
# (cliphist stores each distinct content once, so a preview identifies its entry.)
choice=$(cut -f2- <<<"$history" | spot "$here/clipboard.toml") || exit 0
[[ -n $choice ]] || exit 0

entry=$(PICK=$choice awk -F'\t' '{ p = $0; sub(/^[^\t]*\t/, "", p); if (p == ENVIRON["PICK"]) { print; exit } }' <<<"$history")
[[ -n $entry ]] && cliphist decode <<<"$entry" | wl-copy
