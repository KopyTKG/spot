#!/bin/bash
# askpass.sh — ask for a password in a spot dialog instead of a terminal prompt.
# It is the "askpass helper" that sudo and ssh call: they run it with the prompt as $1 and
# read the password from its stdout.
#
#   SUDO_ASKPASS=/path/to/askpass.sh sudo -A systemctl restart nginx
#   SSH_ASKPASS=/path/to/askpass.sh SSH_ASKPASS_REQUIRE=force ssh host     # key passphrases
#
# root.sh wraps the sudo case and shows *why* a password is wanted. The password is
# printed to stdout and never written anywhere; Esc or an empty password fails the call.
here=$(dirname "$(readlink -f "$0")")

pw=$(SPOT_TITLE="${ASKPASS_TITLE:-Password}" \
     SPOT_FOOTER="${ASKPASS_REASON:+To: $ASKPASS_REASON}${ASKPASS_REASON:+ · }${1:-}" \
     spot "$here/askpass.toml") || exit 1
[[ -n $pw ]] || exit 1
printf '%s\n' "$pw"
