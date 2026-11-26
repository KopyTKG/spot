# AUR packages

| Package | Source |
| --- | --- |
| `spot-bin` | the prebuilt `spot-linux-x64` binary from the GitHub release |
| `spot-git` | built with `go` from the repository head (`pkgver` from `git describe`) |

Both depend on `gtk4` and `gtk4-layer-shell` and install `/usr/bin/spot`; they
conflict with each other.

The PKGBUILDs here are the source of truth. A tag build on Jenkins (`.jenkins/Jenkinsfile`) builds the binary and creates the GitHub release. **Publishing to the AUR is always manual**, from a machine with the AUR SSH key (it can't be done from CI).

## Releasing

1. `just tag 0.1.0 && git push origin v0.1.0` — Jenkins builds and creates the release.
2. Once the release exists, on your machine:

```sh
just release 0.1.0        # tag, push tag to the `github` remote, build, gh release create
just aur-check            # makepkg both packages locally
just aur-publish          # fill spot-bin version + checksums, push both to the AUR
```

Both `aur` recipes take the last release tag; pass a version to publish another one
(`just aur-publish 0.1.0`). Commit the PKGBUILDs afterwards: their checksums only exist once
the release does.

`just aur` does the version/checksum edit alone so the diff can be
inspected first. `spot-git` needs no version bump: its `pkgver()` follows git,
but it is pushed alongside so PKGBUILD changes reach the AUR.

## First-time setup

Register at https://aur.archlinux.org, add an SSH key to the account and map it
in `~/.ssh/config` (`Host aur.archlinux.org` / `User aur` / `IdentityFile ~/.ssh/aur`).
Pushing to a non-existent AUR repo creates the package. `.SRCINFO` is generated
by `just aur-publish` via `makepkg --printsrcinfo`.

## Jenkins

Job config: pipeline from SCM, Script Path `.jenkins/Jenkinsfile`, build tags
too. The stages run in a container built from
`.jenkins/Dockerfile` (Arch Linux), so the agent only needs Docker, a Jenkins user allowed to use it
and the Docker Pipeline plugin. One credential: `spot-github-token` (secret text, contents:write on
KopyTKG/spot). The Go build and module caches live in the Docker volume `spot-go-cache` (shared by all tags
and branches), so only the first build compiles the GTK bindings from scratch; remove the volume to
force a cold build. Re-running a failed tag build is safe: the release script
replaces assets instead of duplicating them. If Jenkins is down,
`just release <ver>` does the same from your machine (needs `gh`).

The release binary is linked against the Arch libraries of the build container and needs GLib 2.88 or
newer. If a release ever misbehaves after a library update, rebuild it locally with `just package`.
