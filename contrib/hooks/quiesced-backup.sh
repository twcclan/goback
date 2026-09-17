#!/bin/sh
# Runs "goback commit new" between a pre and a post command and guarantees
# the post command from outside goback: the trap fires on every exit of
# this shell, so the application is released even when goback itself is
# killed. goback's own --post-hook covers every exit goback controls; this
# wrapper covers the rest.
#
#   PRE_HOOK='pause-writes' POST_HOOK='resume-writes' \
#     contrib/hooks/quiesced-backup.sh --set world /srv/app
#
# PRE_HOOK and POST_HOOK are shell commands; everything else is passed to
# goback commit new.
set -u

: "${PRE_HOOK:?set PRE_HOOK to the command that quiesces the application}"
: "${POST_HOOK:?set POST_HOOK to the command that releases it}"

sh -c "$PRE_HOOK" || exit 1

trap 'sh -c "$POST_HOOK"' EXIT INT TERM

goback commit new "$@"
