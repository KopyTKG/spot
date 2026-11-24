#!/bin/bash
# emoji.sh — emoji picker: a collapsible list, one folded group per Unicode category.
# Type to search every group at once ("grin", "heart eyes", "flag cz"); Enter copies the
# emoji to the clipboard. With python-emoji installed, chat shortcodes work as well
# (":rofl:", ":joy:", ":+1:").
#
#   emoji.sh          the picker
#   emoji.sh list     print the rows (GROUP<TAB>EMOJI  name) and exit
#
# Needs the Unicode emoji list (Arch: unicode-emoji, file /usr/share/unicode/emoji/
# emoji-test.txt) and wl-clipboard. To paste straight into the focused window instead of
# only copying, send Shift+Insert after the copy (wtype -M shift -k Insert, or your
# compositor's own key-sending command).
here=$(dirname "$(readlink -f "$0")")
DATA=${EMOJI_DATA:-/usr/share/unicode/emoji/emoji-test.txt}

rows() {                                                 # Group<TAB>emoji  name
  awk '
    /^# group: / { g = substr($0, 10); next }
    /; fully-qualified/ && g != "Component" {
      sub(/^[^#]*# /, "")                                # "😀 E1.0 grinning face"
      emoji = $1; $1 = ""; $2 = ""; sub(/^ +/, "")       # drop the emoji and the E-version
      printf "%s\t%s  %s\n", g, emoji, $0
    }' "$DATA"
}

with_shortcodes() {                                      # append :shortcodes: when python-emoji exists
  python3 -c '
import sys
try:
    from emoji import EMOJI_DATA
except Exception:
    sys.stdout.write(sys.stdin.read()); raise SystemExit
for line in sys.stdin:
    line = line.rstrip("\n")
    group, _, rest = line.partition("\t")
    ch, _, name = rest.partition("  ")
    info = EMOJI_DATA.get(ch) or EMOJI_DATA.get(ch.replace("️", "")) or {}
    codes = []
    for c in [info.get("en", "")] + list(info.get("alias", [])):
        c = c.strip(":").lower()
        if c and c not in codes and c.replace("_", " ") != name.lower():
            codes.append(c)
    print(line + ("  " + " ".join(":%s:" % c for c in codes[:6]) if codes else ""))'
}

[[ ${1:-} == list ]] && { rows | with_shortcodes; exit 0; }
[[ -r $DATA ]] || { notify-send -u critical "emoji.sh" "No emoji data: install unicode-emoji" 2>/dev/null; exit 1; }

row=$(rows | with_shortcodes | spot "$here/emoji.toml") || exit 0   # the row, without its group
[[ -n $row ]] || exit 0
printf '%s' "${row%% *}" | wl-copy                       # the emoji is everything before the first space
