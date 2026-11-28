#!/bin/bash
# The pretend settings behind the component demos. Nothing real is changed: each
# setting is a small file under $XDG_RUNTIME_DIR/spot-demo.
#
#   demo.sh get <name> [default]    print it
#   demo.sh set <name> <value>      change it
#   demo.sh add <name> <step>       move a number, kept between 0 and 100
#   demo.sh broken                  always fails, to show an unknown state

[[ -n ${XDG_RUNTIME_DIR:-} ]] || { echo "demo.sh: XDG_RUNTIME_DIR is not set" >&2; exit 1; }
dir=$XDG_RUNTIME_DIR/spot-demo
mkdir -p -m 700 "$dir" || exit 1
name=${2:-}
[[ $name =~ ^[a-z0-9-]*$ ]] || { echo "demo.sh: bad name" >&2; exit 2; }
get() { if [[ -f $dir/$name ]]; then cat "$dir/$name"; else echo "$1"; fi; }

case ${1:-} in
  get)    get "${3:-off}" ;;
  set)    printf '%s\n' "${3:-}" > "$dir/$name" ;;
  add)    now=$(get 50); now=$(( now + ${3:-10} )); (( now > 100 )) && now=0; (( now < 0 )) && now=100
          echo "$now" > "$dir/$name" ;;
  broken) echo "demo.sh: this one cannot be read, on purpose" >&2; exit 1 ;;
  *)      echo "usage: demo.sh get|set|add|broken" >&2; exit 2 ;;
esac
