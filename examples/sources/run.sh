#!/bin/bash
# run.sh <command line> — a launcher source: run what you typed, with completions from PATH.
#
#   [[sources]]                      # in a launcher, behind a prefix:
#   name     = "run"
#   prefix   = ">"
#   exec     = "run.sh {query}"
#   activate = "sh -c {value}"
#
#   [[sources]]                      # or as the default of a bare run bar (run.toml):
#   name     = "run"
#   default  = true
#   exec     = "run.sh {query}"
#   activate = "sh -c {value}"
#
# First row: exactly what you typed. Below it: programs on your PATH that start with the
# first word (arguments you typed are kept). A terminal program needs a terminal:
# activate = "xdg-terminal-exec sh -c {value}" (or a wrapper that retries in one).
q=$*
[ -n "$q" ] || exit 0
first=${q%% *}
rest=""; case $q in *" "*) rest=" ${q#* }" ;; esac
printf 'run: %s\t%s\t%s\n' "$q" "$q" "as typed"
bash -c 'compgen -c | sort -u | grep -i -m 25 -- "^$0"' "$first" | while read -r cmd; do
  [ "$cmd" = "$first" ] && [ -z "$rest" ] && continue
  printf '%s\t%s\t\n' "$cmd$rest" "$cmd$rest"
done
