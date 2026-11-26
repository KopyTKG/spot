# AUR packages

| Package | Source |
| --- | --- |
| `spot-bin` | the prebuilt `spot-linux-x64` binary from the GitHub release |
| `spot-git` | built with `go` from the repository head (`pkgver` from `git describe`) |

Both depend on `gtk4` and `gtk4-layer-shell` and install `/usr/bin/spot`; they
conflict with each other.

The PKGBUILDs here are the source of truth. A tag build on Jenkins (`.jenkins/Jenkinsfile`) builds
the binary, creates the GitHub release, publishes both packages to the AUR and commits the updated
PKGBUILDs back to `main` (`ci/aur-release.sh`).

## Releasing

```sh
just tag 0.1.0            # Jenkins does the rest
git pull                  # once it is done: the PKGBUILD commit
```

Version and checksums are filled in after the release because the checksums cover the released
binary and the source archive of the tag.

### By hand

If the AUR stage fails, or without Jenkins, from a machine with an AUR SSH key:

```sh
just release 0.1.0        # only without Jenkins: tag, push, build, gh release create (needs gh)
just aur-check            # makepkg both packages locally
just aur-publish          # fill spot-bin version + checksums, push both to the AUR
```

Both `aur` recipes take the last release tag; pass a version for another one. `just aur` does the
version/checksum edit alone so the diff can be inspected first. Commit the PKGBUILDs afterwards.
`DRY_RUN=1` stops `aur-publish` before it pushes. `spot-git` needs no version bump (its `pkgver()`
follows git) but is pushed alongside so PKGBUILD changes reach the AUR.

## First-time setup

Register at https://aur.archlinux.org, add an SSH key to the account and map it
in `~/.ssh/config` (`Host aur.archlinux.org` / `User aur` / `IdentityFile ~/.ssh/aur`).
Pushing to a non-existent AUR repo creates the package. `.SRCINFO` is generated
by `just aur-publish` via `makepkg --printsrcinfo`.

## Jenkins

Job config: pipeline from SCM, Script Path `.jenkins/Jenkinsfile`, build tags
too. The stages run in a container built from
`.jenkins/Dockerfile` (Arch Linux), so the agent only needs Docker, a Jenkins user allowed to use it
and the Docker Pipeline plugin. Two credentials: `spot-github-token` (secret text, contents:write on
KopyTKG/spot, allowed to push to `main`) and `aur-ssh-key` (SSH username with private key, user
`aur`, its public half on the AUR account). The AUR host keys are pinned in
`.jenkins/aur_known_hosts`. The Go build and module caches live in the Docker volume `spot-go-cache` (shared by all tags
and branches), so only the first build compiles the GTK bindings from scratch; remove the volume to
force a cold build. Re-running a failed tag build is safe: the release script
replaces assets instead of duplicating them. If Jenkins is down,
`just release <ver>` does the same from your machine (needs `gh`).

The release binary is linked against the Arch libraries of the build container and needs GLib 2.88 or
newer. If a release ever misbehaves after a library update, rebuild it locally with `just package`.
