# The host command holds owner.lock. Kernel lock release also covers SIGKILL
# and terminal loss; the watchdog stops the real master even if Docker exec
# disconnects without terminating its container-side process.
set -eu
dir=$1
destination=$2
exec 9<"$dir/owner.lock"
exec 8<"$dir/master.lock"
flock -x 8
# A controller may disappear before Docker even starts this exec. Never start a
# fresh master after its owner has gone away.
if flock -n 9; then exit 1; fi
master=
watchdog=
cleanup() {
    trap '' HUP INT TERM
    [ -z "$master" ] || kill -TERM "$master" 2>/dev/null || :
    [ -z "$watchdog" ] || kill -TERM "$watchdog" 2>/dev/null || :
    [ -z "$master" ] || wait "$master" 2>/dev/null || :
    [ -z "$watchdog" ] || wait "$watchdog" 2>/dev/null || :
}
trap cleanup EXIT
trap 'exit 129' HUP
trap 'exit 130' INT
trap 'exit 143' TERM
ssh -M -N -S "$dir/socket" -o ControlPersist=no -o ForkAfterAuthentication=no "$destination" <&0 &
master=$!
(
    trap - HUP INT TERM EXIT
    while ! flock -n 9; do sleep 1; done
    kill -TERM "$master" 2>/dev/null || :
) &
watchdog=$!
wait "$master"
