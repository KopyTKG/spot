#!/usr/bin/env bash
# Creates (or updates) the GitHub release for a tag and uploads its assets.
#
#   GH_TOKEN=... ci/github-release.sh v0.1.0 dist/spot-linux-x64
#
# Safe to run twice: an existing release is updated rather than duplicated, and
# an asset that is already attached is replaced. Re-running a release job is
# what one does after fixing whatever broke it, so it has to be a no-op rather
# than a failure.
#
# Needs curl and jq. GITHUB_REPOSITORY defaults to KopyTKG/spot.
set -euo pipefail

tag=${1:?usage: github-release.sh <tag> <asset>...}
shift
repo=${GITHUB_REPOSITORY:-KopyTKG/spot}
: "${GH_TOKEN:?GH_TOKEN must hold a token with contents:write on $repo}"

api="https://api.github.com/repos/$repo"
auth=(-H "Authorization: Bearer $GH_TOKEN" -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28")

# The release notes are the commit subjects since the previous tag.
prev=$(git describe --tags --abbrev=0 "$tag^" 2>/dev/null || true)
notes=$(git log --no-merges --pretty='- %s' ${prev:+"$prev..$tag"} 2>/dev/null | head -50 || true)
[ -n "$notes" ] || notes="Release $tag."

body=$(jq -n --arg tag "$tag" --arg name "spot $tag" --arg notes "$notes" \
  '{tag_name: $tag, name: $name, body: $notes}')

# gh_api METHOD URL [curl args...] — prints the body; on an HTTP error prints
# GitHub's message (curl -f would hide it behind a bare status) and fails.
gh_api() {
  local method=$1 url=$2; shift 2
  local out status
  out=$(mktemp)
  status=$(curl -sS -o "$out" -w '%{http_code}' "${auth[@]}" -X "$method" "$@" "$url")
  if [ "$status" -ge 400 ]; then
    echo "GitHub API $method $url -> HTTP $status: $(jq -r '.message // empty' "$out" 2>/dev/null || cat "$out")" >&2
    [ "$status" -eq 403 ] || [ "$status" -eq 404 ] && echo "hint: the token needs Contents: read and write on $repo" >&2
    rm -f "$out"
    return 1
  fi
  cat "$out"; rm -f "$out"
}

release=$(curl -fsS "${auth[@]}" "$api/releases/tags/$tag" 2>/dev/null || true)
if [ -n "$release" ]; then
  id=$(jq -r .id <<<"$release")
  echo "release $tag exists (id $id); updating it"
else
  echo "creating release $tag"
  release=$(gh_api POST "$api/releases" -d "$body")
  id=$(jq -r .id <<<"$release")
fi

assets=$(curl -fsS "${auth[@]}" "$api/releases/$id/assets?per_page=100")
for file in "$@"; do
  name=$(basename "$file")
  existing=$(jq -r --arg name "$name" '.[] | select(.name == $name) | .id' <<<"$assets")
  if [ -n "$existing" ]; then
    echo "replacing $name"
    curl -fsS "${auth[@]}" -X DELETE "$api/releases/assets/$existing" >/dev/null
  else
    echo "uploading $name"
  fi
  curl -fsS "${auth[@]}" -H "Content-Type: application/octet-stream" \
    --data-binary "@$file" \
    "https://uploads.github.com/repos/$repo/releases/$id/assets?name=$name" >/dev/null
done

echo "https://github.com/$repo/releases/tag/$tag"
