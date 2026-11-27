#!/bin/sh
# Pick a socket and copy its local address. Pass "listening" for those only.
# SPOT chooses the spot to use, e.g. SPOT="just dev" while trying the branch.
set -eu
dir=$(dirname "$(readlink -f "$0")")
spot=${SPOT:-spot}

# No table without rows to put in it: a failure must not look like "no sockets".
# (In a pipe the failure of `get` would be lost.)
errors=$(mktemp)
trap 'rm -f "$errors"' EXIT
if ! rows=$($spot get "sockets.${1:-table}" 2>"$errors"); then
  cat "$errors" >&2
  # Started from a bar or a key, nobody sees stderr.
  [ -t 2 ] || notify-send --app-name=spot -- "Sockets" "$(sed 's/\\/\\\\/g' "$errors")"
  exit 1
fi

row=$(printf '%s\n' "$rows" | cut -f1 | $spot "$dir/sockets.toml") || exit 0
# netid, state, local address, peer
printf '%s' "$row" | awk '{ printf "%s", $3 }' | wl-copy
