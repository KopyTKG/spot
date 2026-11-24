#!/bin/bash
# root.sh <reason> <command...> — run a command as root, asking for the password in a spot
# dialog that says why. sudo -A uses SUDO_ASKPASS (askpass.sh), so there is no terminal
# prompt and no pkexec / polkit window.
#
#   root.sh "restart nginx" systemctl restart nginx
#
# When sudo already has your credentials cached it does not ask at all.
here=$(dirname "$(readlink -f "$0")")
reason=${1:?usage: root.sh <reason> <command...>}
shift
export SUDO_ASKPASS="$here/askpass.sh" ASKPASS_REASON="$reason"
exec sudo -A -- "$@"
