#!/bin/sh
# Pick a socket and copy its local address. Pass "listening" for those only.
# SPOT chooses the spot to use, e.g. SPOT="just dev" while trying the branch.
set -eu
dir=$(dirname "$(readlink -f "$0")")
spot=${SPOT:-spot}
rows=$($spot get "sockets.${1:-table}" | cut -f1)
row=$(printf '%s\n' "$rows" | $spot "$dir/sockets.toml") || exit 0
# netid, state, local address, peer
printf '%s' "$row" | awk '{ printf "%s", $3 }' | wl-copy
