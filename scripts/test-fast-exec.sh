#!/bin/sh
set -eu

# Only app's state-heavy tests use tmpfs. Other packages execute test-created
# scripts, which fail on /dev/shm when it is mounted noexec.
if [ "${1##*/}" = app.test ] && [ -d /dev/shm ] && [ -w /dev/shm ]; then
	TMPDIR=/dev/shm
	export TMPDIR
fi

exec "$@"
