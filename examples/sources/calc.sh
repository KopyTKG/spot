#!/bin/sh
# calc.sh <expression> — a launcher source: one row with the result of a calculation.
#
#   [[sources]]
#   name     = "calc"
#   prefix   = "="
#   exec     = "calc.sh {query}"
#   activate = "wl-copy {value}"
#
# Row: label = value = the answer, comment = what was asked. Needs qalc
# (libqalculate); "5 ft to cm" converts units.
q=$*
[ -n "$q" ] || exit 0
command -v qalc >/dev/null 2>&1 || { echo "qalc is not installed"; exit 0; }
ans=$(qalc -t -- "$q" 2>&1 | head -n 1)
[ -n "$ans" ] && printf '%s\t%s\t%s\n' "$ans" "$ans" "$q"
