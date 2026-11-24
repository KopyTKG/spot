#!/bin/bash
# sockets.sh — the machine's TCP/UDP sockets as a searchable table: `ss` with a GUI.
# Type to filter (every word has to match: "tcp listen 22", "ssh estab", "docker");
# Enter copies the row's local address (addr:port) to the clipboard.
#
#   sockets.sh                 every socket
#   sockets.sh --listening     only what listens / is bound
#   sockets.sh list            print the table (with header) and exit
#   sockets.sh header          print just the column header
#
# The columns are fixed width and monospace. The header is ALSO the `label` of the list in
# sockets.toml, so the two must stay equal: `sockets.sh header` prints the right one.
# Process names come from `ss -p`, which only names *your* sockets without root.
here=$(dirname "$(readlink -f "$0")")
W_PROTO=5 W_STATE=11 W_LOCAL=28 W_PEER=28
LISTEN_ONLY=0

header() { printf '%-*s %-*s %-*s %-*s %s' "$W_PROTO" PROTO "$W_STATE" STATE "$W_LOCAL" LOCAL "$W_PEER" PEER PROCESS; }

# ss -H -tunap:  netid state recv-q send-q local peer [users:(("name",pid=N,fd=M),...)]
rows() {
  ss -H -tunap 2>/dev/null | awk \
    -v wp="$W_PROTO" -v ws="$W_STATE" -v wl="$W_LOCAL" -v we="$W_PEER" -v only="$LISTEN_ONLY" '
    function cut(s, w) { return length(s) > w ? substr(s, 1, w - 1) "~" : s }
    {
      state = $2
      listening = (state == "LISTEN" || state == "UNCONN")
      if (only && !listening) next
      proc = "-"
      if (match($0, /users:\(\("([^"]*)",pid=([0-9]+)/, m)) proc = m[1] "(" m[2] ")"
      port = $5; sub(/.*:/, "", port); if (port !~ /^[0-9]+$/) port = 0
      rank = listening ? 0 : (state == "ESTAB" ? 1 : 2)                  # listeners first
      printf "%d\t%05d\t%-*s %-*s %-*s %-*s %s\n", rank, port, wp, $1, ws, state, wl, cut($5, wl), we, cut($6, we), proc
    }' | sort -t$'\t' -k1,1n -k2,2n -s | cut -f3-
}

while (($#)); do
  case $1 in
    -l|--listening) LISTEN_ONLY=1 ;;
    list)   header; echo; rows; exit 0 ;;
    header) header; echo; exit 0 ;;
    *) echo "usage: sockets.sh [--listening | list | header]" >&2; exit 2 ;;
  esac
  shift
done

total=$(ss -H -tuan 2>/dev/null | wc -l)
choice=$(rows | SPOT_TITLE="Sockets · $total total" spot "$here/sockets.toml") || exit 0
[[ -n $choice ]] || exit 0
read -r _ _ local _ <<<"$choice"                        # the third column is the local address
printf '%s' "$local" | wl-copy
notify-send -u low "Copied $local" 2>/dev/null
