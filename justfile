set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load := false

# Where build output lands. Override with `just dist=/tmp/out package`.
dist := "dist"
repo := "KopyTKG/spot"
aur_packages := "spot-bin spot-git"

# List the available recipes.
default:
    @just --list --unsorted

# --------------------------------------------------------------------------
# Build
# --------------------------------------------------------------------------

# Build ./spot for this machine (needs gtk4, gtk4-layer-shell, go).
build:
    go build -o spot .

# Fail if anything is formatted off-style, then vet.
lint:
    test -z "$(gofmt -l $(git ls-files '*.go'))" || { gofmt -l $(git ls-files '*.go'); exit 1; }
    go vet ./...

# What CI runs.
ci: lint build package

# Release binary: stripped, trimmed paths, PIE.
package:
    mkdir -p {{dist}}
    go build -buildmode=pie -trimpath -ldflags "-s -w" -o {{dist}}/spot-linux-x64 .
    @ls -lh {{dist}}/spot-linux-x64

# Remove build output.
clean:
    rm -rf {{dist}} spot

# --------------------------------------------------------------------------
# Release (Jenkins builds + releases from the tag; the AUR is always manual)
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
# AUR (spot-bin from the release binary, spot-git from the repo head)
# --------------------------------------------------------------------------

# Point spot-bin at a released version, checksums and all.
aur version:
    #!/usr/bin/env bash
    set -euo pipefail
    release="https://github.com/{{repo}}/releases/download/v{{version}}"
    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT

    curl --fail --silent --show-error --location -o "$work/spot" "$release/spot-linux-x64"
    curl --fail --silent --show-error --location -o "$work/LICENSE" \
        "https://raw.githubusercontent.com/{{repo}}/v{{version}}/LICENSE"
    bin=$(sha256sum "$work/spot" | cut -d' ' -f1)
    licence=$(sha256sum "$work/LICENSE" | cut -d' ' -f1)

    file="packaging/aur/spot-bin/PKGBUILD"
    sed -i "s|^pkgver=.*|pkgver={{version}}|; s|^pkgrel=.*|pkgrel=1|" "$file"
    sed -i "0,/^sha256sums=(/{s|^sha256sums=('.*'|sha256sums=('$bin'|}" "$file"
    sed -i "s|^            '.*')|            '$licence')|" "$file"
    echo "$file -> {{version}}"

# Push both AUR packages from this machine, after the release exists. Needs an AUR SSH key.
aur-publish version:
    #!/usr/bin/env bash
    set -euo pipefail
    just aur {{version}}

    work=$(mktemp -d)
    trap 'rm -rf "$work"' EXIT

    for package in {{aur_packages}}; do
        echo "==> $package"
        git clone --quiet "ssh://aur@aur.archlinux.org/$package.git" "$work/$package"
        cp "packaging/aur/$package/PKGBUILD" "$work/$package/PKGBUILD"
        (
            cd "$work/$package"
            makepkg --printsrcinfo > .SRCINFO
            git add PKGBUILD .SRCINFO
            if git diff --cached --quiet; then
                echo "    already up to date"
            else
                git commit -qm "Update to {{version}}"
                git push --quiet
                echo "    pushed"
            fi
        )
    done

# Build the AUR packages locally, to check them before they reach anyone.
aur-check:
    #!/usr/bin/env bash
    set -euo pipefail
    for package in {{aur_packages}}; do
        work=$(mktemp -d)
        cp "packaging/aur/$package/PKGBUILD" "$work/"
        (cd "$work" && makepkg --nodeps --clean)
        echo "built: $(ls "$work"/*.pkg.tar.zst)"
        rm -rf "$work"
    done
