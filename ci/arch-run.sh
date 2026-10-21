#!/usr/bin/env bash
# Runs a just recipe inside an Arch container, so spot is built against the
# same gtk4 / gtk4-layer-shell that Arch users run with (cgo links them).
#
#   ci/arch-run.sh lint
#   ci/arch-run.sh package
#
# Needs docker. The workspace is mounted at /src; whatever the container writes
# is chowned back to the calling user.
set -euo pipefail

[ $# -ge 1 ] || { echo "usage: arch-run.sh <just recipe>..." >&2; exit 2; }

docker run --rm \
    -v "$PWD:/src" -w /src \
    -e HOST_UID="$(id -u)" -e HOST_GID="$(id -g)" \
    -e GOCACHE=/src/.cache/go-build -e GOPATH=/src/.cache/go \
    archlinux:base-devel \
    bash -euo pipefail -c '
        pacman -Syu --noconfirm --needed go gtk4 gtk4-layer-shell just git >/dev/null
        git config --global --add safe.directory /src
        trap "chown -R $HOST_UID:$HOST_GID /src" EXIT
        just "$@"
    ' arch-run "$@"
