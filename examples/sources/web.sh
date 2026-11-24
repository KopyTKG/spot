#!/bin/sh
# web.sh <query> — a launcher source: one row per search engine for what you typed.
#
#   [[sources]]
#   name     = "web"
#   prefix   = "@"
#   exec     = "web.sh {query}"
#   activate = "xdg-open {value}"
#
# Row: label = the engine, value = the search URL, comment = what is searched.
# Edit the list below to taste (%s is the URL-encoded query). Needs jq.
q=$*
[ -n "$q" ] || exit 0
enc=$(jq -rn --arg q "$q" '$q|@uri')
while IFS='|' read -r name url; do
  [ -n "$name" ] || continue
  printf '%s\t%s\t%s\n' "$name" "$(printf '%s' "$url" | sed "s|%s|$enc|")" "search for: $q"
done <<'ENGINES'
DuckDuckGo|https://duckduckgo.com/?q=%s
Google|https://www.google.com/search?q=%s
Arch Wiki|https://wiki.archlinux.org/index.php?search=%s
Wikipedia|https://en.wikipedia.org/w/index.php?search=%s
GitHub|https://github.com/search?q=%s
YouTube|https://www.youtube.com/results?search_query=%s
ENGINES
