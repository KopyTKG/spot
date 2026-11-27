set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load := false

# Where build output lands. Override with `just dist=/tmp/out package`.
dist := "dist"
repo := "KopyTKG/spot"
aur_packages := "spot-bin spot-git"
# Where the AUR repositories are cloned from and pushed to.
aur_remote := env_var_or_default("AUR_REMOTE", "ssh://aur@aur.archlinux.org")

# List the available recipes.
default:
    @just --list --unsorted

# --------------------------------------------------------------------------
# Build
# --------------------------------------------------------------------------

# Version baked into the binary: the latest tag, e.g. 0.2.0 or 0.2.0-3-gabc123.
build_version := `git describe --tags --always --dirty 2>/dev/null | sed 's/^v//' || echo dev`
ldflags := "-X main.version=" + build_version

# The last release tag without the v, what the AUR recipes publish by default.
# (build_version also counts commits since it, which no release is named after.)
release_version := `git describe --tags --abbrev=0 --match 'v*' 2>/dev/null | sed 's/^v//' || true`

# Build ./spot for this machine (needs gtk4, gtk4-layer-shell, go), and the mod
# host beside it: a static program, the sandbox grants it nothing but itself.
build:
    go build -ldflags "{{ldflags}}" -o spot .
    CGO_ENABLED=0 go build -o spot-mod-host ./cmd/spot-mod-host

# Run the unit tests (no display needed).
test:
    go test ./...

# The mod tests, failing if any is skipped: Jenkins has no Landlock and skips
# the sandbox's, so run this here before releasing anything that touches mods.
test-mods:
    #!/usr/bin/env bash
    set -uo pipefail
    out=$(go test -count=1 -v ./internal/mod ./internal/sandbox ./internal/modhost ./internal/modcheck ./internal/parser ./internal/scan ./internal/modtest ./internal/approval ./internal/modd ./internal/daemon ./internal/config ./internal/scheduler . 2>&1); status=$?
    echo "$out" | grep -E '^(ok|FAIL)|^\s*--- (SKIP|FAIL)|_test\.go:[0-9]+:'
    skipped=$(grep -c -- '--- SKIP' <<<"$out")
    echo "$(grep -c -- '--- PASS' <<<"$out") passed, $skipped skipped"
    [ "$status" -eq 0 ] || exit "$status"
    [ "$skipped" -eq 0 ] || { echo "not checked: $skipped tests were skipped" >&2; exit 1; }

# Fail if anything is formatted off-style, then vet.
lint:
    test -z "$(gofmt -l $(git ls-files '*.go'))" || { gofmt -l $(git ls-files '*.go'); exit 1; }
    go vet ./...

# What CI runs.
ci: lint test build package

# Release binary: stripped, trimmed paths, PIE.
package:
    mkdir -p {{dist}}
    go build -buildmode=pie -trimpath -ldflags "-s -w {{ldflags}}" -o {{dist}}/spot-linux-x64 .
    # The mod host must be static, so not pie: its sandbox grants it only itself.
    CGO_ENABLED=0 go build -buildmode=exe -trimpath -ldflags "-s -w" -o {{dist}}/spot-mod-host-linux-x64 ./cmd/spot-mod-host
    @! readelf -l {{dist}}/spot-mod-host-linux-x64 | grep -q INTERP || { echo "spot-mod-host is not static" >&2; exit 1; }
    @ls -lh {{dist}}/spot-linux-x64 {{dist}}/spot-mod-host-linux-x64

# Remove build output.
clean:
    rm -rf {{dist}} spot spot-mod-host

# Run this build beside your installed spot, with a daemon and socket of its own:
# `just dev get sockets.count`. Your installed spot is not touched.
[positional-arguments]
[no-exit-message]
dev *args:
    @SPOT_SOCKET="$XDG_RUNTIME_DIR/spot-dev.sock" XDG_STATE_HOME="$XDG_RUNTIME_DIR/spot-dev-state" ./spot "$@"

# --------------------------------------------------------------------------
# Release (Jenkins builds, releases and publishes to the AUR from the tag)
# --------------------------------------------------------------------------

# Tag the current commit as v<version>; push it and Jenkins builds + releases.
tag version:
    @test -z "$(git status --porcelain)" || { echo "working tree is dirty"; exit 1; }
    git tag -a "v{{version}}" -m "spot v{{version}}"
    @echo "tagged v{{version}}"
    git push origin v{{version}}
    @echo "tag v{{version}} pushed"

# Manual fallback when CI is unavailable: tag, push, build, gh release (needs gh).
release version: (tag version)
    git push origin "v{{version}}"
    just package
    gh release create "v{{version}}" --repo {{repo}} --title "v{{version}}" --generate-notes \
        {{dist}}/spot-linux-x64

# --------------------------------------------------------------------------
# AUR (spot-bin from the release binary, spot-git from the repo head).
# Jenkins publishes after a release (ci/aur-release.sh); these also work by hand.
# --------------------------------------------------------------------------

# Point spot-bin at a released version (default: the last tag), checksums and all.
aur version=release_version:
    #!/usr/bin/env bash
    set -euo pipefail
    [[ -n "{{version}}" ]] || { echo "no release tag found; pass a version"; exit 1; }
    release="https://github.com/{{repo}}/releases/download/v{{version}}"
    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT

    curl --fail --silent --show-error --location -o "$work/spot" "$release/spot-linux-x64"
    curl --fail --silent --show-error --location -o "$work/host" "$release/spot-mod-host-linux-x64"
    curl --fail --silent --show-error --location -o "$work/source.tar.gz" \
        "https://github.com/{{repo}}/archive/refs/tags/v{{version}}.tar.gz"
    bin=$(sha256sum "$work/spot" | cut -d' ' -f1)
    host=$(sha256sum "$work/host" | cut -d' ' -f1)
    source=$(sha256sum "$work/source.tar.gz" | cut -d' ' -f1)

    file="packaging/aur/spot-bin/PKGBUILD"
    sed -i "s|^pkgver=.*|pkgver={{version}}|; s|^pkgrel=.*|pkgrel=1|" "$file"
    sed -i "0,/^sha256sums=(/{s|^sha256sums=('.*'|sha256sums=('$bin'|}" "$file"
    sed -i "s|^            '[^']*'$|            '$host'|; s|^            '.*')|            '$source')|" "$file"
    echo "$file -> {{version}}"

    # spot-git's pkgver() only runs at build time; the AUR listing reads the
    # static pkgver from .SRCINFO, so seed it with what pkgver() would print.
    git fetch --quiet --tags origin || true
    gitver=$(git describe --long --tags --abbrev=7 "v{{version}}" | sed 's/^v//; s/\([^-]*-g\)/r\1/; s/-/./g')
    file="packaging/aur/spot-git/PKGBUILD"
    sed -i "s|^pkgver=.*|pkgver=$gitver|; s|^pkgrel=.*|pkgrel=1|" "$file"
    echo "$file -> $gitver"

# Push both AUR packages from this machine, after the release exists. Needs an AUR SSH key.
aur-publish version=release_version:
    #!/usr/bin/env bash
    set -euo pipefail
    just aur {{version}}

    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT

    for package in {{aur_packages}}; do
        echo "==> $package"
        git clone --quiet "{{aur_remote}}/$package.git" "$work/$package"
        cp "packaging/aur/$package/PKGBUILD" "$work/$package/PKGBUILD"
        (
            cd "$work/$package"
            makepkg --printsrcinfo > .SRCINFO
            git add PKGBUILD .SRCINFO
            if git diff --cached --quiet; then
                echo "    already up to date"
            else
                git commit -qm "Update to {{version}}"
                if [[ -n "${DRY_RUN:-}" ]]; then
                    echo "    dry run, would push:"
                    git --no-pager show --stat --format='      %an: %s' HEAD
                else
                    git push --quiet
                    echo "    pushed"
                fi
            fi
        )
    done

# Build the AUR packages locally, to check them before they reach anyone.
aur-check *packages=aur_packages:
    #!/usr/bin/env bash
    set -euo pipefail
    for package in {{packages}}; do
        work=$(mktemp -d)
        cp "packaging/aur/$package/PKGBUILD" "$work/"
        (cd "$work" && makepkg --nodeps --clean)
        echo "built: $(ls "$work"/*.pkg.tar.zst)"
        rm -rf "$work"
    done
