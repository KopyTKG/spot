# Build environment for spot's Jenkins pipeline.
#
# Arch, because spot ships through the AUR and the gotk4 bindings need a recent
# GLib (2.88 or newer) than Debian stable has.
FROM archlinux:base-devel

# --disable-sandbox: pacman's own sandbox does not work inside a build container.
RUN pacman -Syu --noconfirm --needed --disable-sandbox \
        go git curl jq just openssh gtk4 gtk4-layer-shell gobject-introspection \
    && pacman -Scc --noconfirm --disable-sandbox

# Jenkins runs the container as its own user, which has no home in the image.
# /cache is world-writable so that a volume mounted there starts out that way.
RUN install -d -m 1777 /cache
# Publishing to the AUR uses ssh, which needs a passwd entry for that user:
# ci/aur-release.sh adds one. Throwaway build container only.
RUN chmod a+w /etc/passwd
ENV HOME=/tmp GOCACHE=/cache/go-build GOPATH=/cache/go
