#!/usr/bin/env bash
# Launcher for the spot 0.2.0 demos: pick one from a spot list, see its output.
#
#   ./demo.sh
#
# Needs spot >= 0.2.0 on PATH (spot --version) and jq for the form demo.
set -u
cd "$(dirname "$0")"

command -v spot >/dev/null || { echo "spot not found" >&2; exit 1; }
spot --version

demos=(
  "01-form      all input widgets, validation, JSON output"
  "02-tiles     two tile sections, {_active}, custom fade"
  "03-list      dmenu-style list from stdin"
  "04-exec      exec submit with on_error = show"
)

while :; do
  pick=$(printf '%s\n' "${demos[@]}" "quit" \
    | SPOT_TITLE="spot demos" SPOT_PLACEHOLDER="Pick a demo..." spot ../menu.toml) || break
  name=${pick%% *}
  case $name in
    01-form)  spot 01-form.toml | jq . ;;
    02-tiles) spot 02-tiles.toml ;;
    03-list)  printf '%s\n' alpha beta gamma delta epsilon | spot 03-list.toml ;;
    04-exec)  spot 04-exec.toml ;;
    *)        break ;;
  esac
  echo "exit status: $?"
done
