#!/usr/bin/env bash
# Publishes a released tag to the AUR and commits the updated PKGBUILDs to main.
#
#   AUR_KEY=<ssh key file> GH_TOKEN=... ci/aur-release.sh v0.1.0
#
# Run by Jenkins after the GitHub release, inside the .jenkins/Dockerfile image.
# GH_TOKEN needs contents:write on the repository and to be allowed to push to
# main. Safe to run twice: nothing is pushed where nothing changed. A tag that is
# not the newest one is skipped, so re-running an old build cannot downgrade the
# packages.
#
# DRY_RUN=1 does everything except push, and needs neither key nor token (set
# AUR_REMOTE=https://aur.archlinux.org to clone without a key).
set -euo pipefail

tag=${1:?usage: aur-release.sh <tag>}
version=${tag#v}
repo=${REPO_URL:-https://github.com/KopyTKG/spot.git}
here=$PWD

newest=$(git ls-remote --tags --refs "$repo" 'v*' | sed 's|.*refs/tags/||' | sort -V | tail -1)
if [[ $tag != "$newest" ]]; then
  echo "$tag is not the newest tag ($newest): leaving the AUR alone"
  exit 0
fi

# The commits are made in the name of whoever made the tagged commit.
GIT_AUTHOR_NAME=$(git log -1 --format=%an "$tag")
GIT_AUTHOR_EMAIL=$(git log -1 --format=%ae "$tag")
export GIT_AUTHOR_NAME GIT_AUTHOR_EMAIL
export GIT_COMMITTER_NAME=$GIT_AUTHOR_NAME GIT_COMMITTER_EMAIL=$GIT_AUTHOR_EMAIL

# ssh refuses to run as a user with no passwd entry, which Jenkins' user is here.
if ! getent passwd "$(id -u)" >/dev/null; then
  echo "ci:x:$(id -u):$(id -g)::/tmp:/bin/bash" >> /etc/passwd
fi
if [[ -z ${DRY_RUN:-} ]]; then
  export GIT_SSH_COMMAND="ssh -i ${AUR_KEY:?path of the AUR ssh key} -o IdentitiesOnly=yes \
-o UserKnownHostsFile=$here/.jenkins/aur_known_hosts -o StrictHostKeyChecking=yes"
fi

# Nothing goes to the AUR unless the binary package builds and installs its files.
just aur "$version"
just aur-check spot-bin
just aur-publish "$version"

# The same edit on main, which may have moved on since the tag.
git fetch --quiet "$repo" main
work=$(mktemp -d)
trap 'git -C "$here" worktree remove --force "$work/main" 2>/dev/null || true; rm -rf "$work"' EXIT
git worktree add --quiet --detach "$work/main" FETCH_HEAD
cd "$work/main"
just aur "$version"
if git diff --quiet -- packaging/aur; then
  echo "main already has the $version PKGBUILDs"
  exit 0
fi
git commit --quiet -m "publishing pkgbuild for AUR $version" -- packaging/aur
git --no-pager show --stat --oneline HEAD
if [[ -n ${DRY_RUN:-} ]]; then
  echo "dry run: not pushing to main"
  exit 0
fi
: "${GH_TOKEN:?GH_TOKEN must hold a token with contents:write}"
# The token goes to git through a helper, so it is in neither the URL nor the log.
git -c credential.helper= \
    -c credential.helper='!f() { echo username=x-access-token; echo "password=$GH_TOKEN"; }; f' \
    push --quiet "$repo" HEAD:main
echo "pushed to main"
