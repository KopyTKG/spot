#!/bin/sh
# files.sh <name> — a launcher source: files and folders under $HOME whose name matches.
#
#   [[sources]]
#   name     = "files"
#   prefix   = "."
#   exec     = "files.sh {query}"
#   activate = "xdg-open {value}"
#
# Row: label = the file name, value = the full path, comment = its folder (with ~).
# Needs fd. Add --exclude flags for folders you never want listed.
q=$*
[ -n "$q" ] || exit 0
command -v fd >/dev/null 2>&1 || { echo "fd is not installed"; exit 0; }
fd --ignore-case --max-results 40 --exclude .git --exclude node_modules --exclude .cache -- "$q" "$HOME" 2>/dev/null |
  awk -v home="$HOME" '{
    path = $0; sub(/\/$/, "", path)
    n = split(path, parts, "/"); name = parts[n]
    dir = path; sub(/\/[^\/]*$/, "", dir)
    if (index(dir, home) == 1) dir = "~" substr(dir, length(home) + 1)
    printf "%s\t%s\t%s\n", name, path, dir
  }'
