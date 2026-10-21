# AUR packages

| Package | Source |
| --- | --- |
| `spot-bin` | the prebuilt `spot-linux-x64` binary from the GitHub release |
| `spot-git` | built with `go` from the repository head (`pkgver` from `git describe`) |

Both depend on `gtk4` and `gtk4-layer-shell` and install `/usr/bin/spot`; they
conflict with each other.

The PKGBUILDs here are the source of truth. Releases are manual — no CI.

## Releasing

```sh
just release 0.1.0        # tag, push tag to the `github` remote, build, gh release create
just aur-check            # makepkg both packages locally
just aur-publish 0.1.0    # fill spot-bin version + checksums, push both to the AUR
```

`just aur 0.1.0` does the version/checksum edit alone so the diff can be
inspected first. `spot-git` needs no version bump: its `pkgver()` follows git,
but it is pushed alongside so PKGBUILD changes reach the AUR.

## First-time setup

Register at https://aur.archlinux.org, add an SSH key to the account and map it
in `~/.ssh/config` (`Host aur.archlinux.org` / `User aur` / `IdentityFile ~/.ssh/aur`).
Pushing to a non-existent AUR repo creates the package. `.SRCINFO` is generated
by `just aur-publish` via `makepkg --printsrcinfo`.

Needs a `github` git remote: `git remote add github git@github.com:KopyTKG/spot.git`.
