#!/bin/bash
# OpenCode owns its backend and credential format. Shared auth is updated only
# when it still matches the snapshot this command imported.
set -euo pipefail
umask 077
binary=$1 shared=$2 data=$3
shift 3

fail() { printf 'OpenCode auth: %s\n' "$1" >&2; exit 1; }

# Metadata and backend/install administration retain their native behavior.
if [[ $# == 1 && $1 =~ ^(--help|-h|--version|-v)$ ]] || [[ ${1-} == debug && ${2-} == paths ]]; then
    exec "$binary" "$@"
fi
case ${1-} in service|serve|pair|upgrade|update|uninstall) exec "$binary" "$@" ;; esac
for arg do
    [[ $arg == -- ]] && break
    case $arg in --server|--server=*|--standalone|--standalone=*)
        fail 'External/standalone server selection cannot use shared auth synchronization.' ;;
    esac
done

owner=$(stat -Lc '%d-%i' "$data")
store=$shared/store-$owner
[[ ! -L $store && ! -L $shared/lock ]] || fail 'Invalid auth state path.'
mkdir -p -- "$store"
[[ ! -L $store/lock ]] || fail 'Invalid local auth lock.'
# Importing into an in-use database could replace a running client's auth. Only
# this backing store is exclusive; different sessions do not hold a shared lock.
exec 8>"$store/lock"
flock -n 8 || fail 'Already in use in this Devbox session. Close it, then retry.'
exec 9>"$shared/lock"
snapshot=$shared/credentials.json
pending=$store/pending
work=$(mktemp -d "$store/.sync.XXXXXX")
write_back=false
keep=false

canonical() { jq -S 'sort_by(.id)' "$1" >"$2" 2>"$work/log"; }
# Keep auth subprocesses in the terminal's process group so hangup reaches them;
# the wrapper cannot run its signal trap until its foreground child exits.
export_local() {
    timeout --foreground 30s "$binary" auth export >"$work/local.json" 2>"$work/log" &&
        jq -e 'type == "array"' "$work/local.json" >/dev/null 2>"$work/log"
}
read_shared() {
    [[ ! -L $snapshot ]] || return 1
    if [[ -e $snapshot ]]; then
        cp -- "$snapshot" "$1" || return 1
    else
        printf '[]\n' >"$1" || return 1
    fi
    jq -e 'type == "array"' "$1" >/dev/null 2>"$work/log"
}

commit() {
    export_local && canonical "$work/local.json" "$work/local.sorted" || return 1
    # An unchanged client must not revert another session's update.
    cmp -s "$work/baseline.sorted" "$work/local.sorted" && return 0
    flock -w 30 9 || return 1
    result=0
    if ! read_shared "$work/current.json" || ! canonical "$work/current.json" "$work/current.sorted"; then
        result=1
    elif cmp -s "$work/current.sorted" "$work/local.sorted"; then
        : # Another client already published this same result.
    elif cmp -s "$work/baseline.sorted" "$work/current.sorted"; then
        sync -f "$work/local.json" && mv -f -- "$work/local.json" "$snapshot" && sync -f "$shared" || result=1
    else
        # Retain rejected values, not just the database cache that the next pull
        # will replace. The first writer wins; no implicit merge or overwrite.
        conflict=$(mktemp "$store/conflict.XXXXXX") || result=1
        if [[ $result == 0 ]]; then
            if sync -f "$work/local.json" && mv -f -- "$work/local.json" "$conflict" && sync -f "$store"; then
                printf 'OpenCode auth changed in another session; shared auth was not overwritten. Recovery copy: %s\n' "$conflict" >&2
                result=2
            else
                result=1
            fi
        fi
    fi
    flock -u 9
    return "$result"
}

finish() {
    status=$?
    trap - EXIT
    # A terminal hangup must not interrupt credential publication halfway through.
    trap '' INT TERM HUP
    if $write_back; then
        result=0
        commit || result=$?
        if [[ $result == 1 ]]; then
            keep=true
            printf 'OpenCode auth write-back failed. Reopen this session to retry.\n' >&2
        fi
        [[ $result == 0 || $status != 0 ]] || status=1
    fi
    if ! $keep && ! rm -rf -- "$work"; then
        printf 'OpenCode auth temporary-file cleanup failed.\n' >&2
        [[ $status != 0 ]] || status=1
    fi
    exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

# An interrupted run keeps its baseline with this backing store. Recovery uses
# the same compare-and-swap rule, never the old unconditional write-back.
if [[ -e $pending || -L $pending ]]; then
    [[ -d $pending && ! -L $pending && -f $pending/baseline.sorted && ! -L $pending/baseline.sorted ]] || fail 'Invalid pending auth state.'
    rm -rf -- "$work"
    work=$pending
    result=0
    commit || result=$?
    if [[ $result == 1 ]]; then
        keep=true
        fail 'Could not recover interrupted write-back. Retry this session.'
    fi
    rm -rf -- "$work"
    work=$(mktemp -d "$store/.sync.XXXXXX")
fi
flock -w 30 9 || fail 'Could not lock shared auth.'
read_shared "$work/desired.json" || fail 'Invalid shared credential snapshot.'
canonical "$work/desired.json" "$work/baseline.sorted" || fail 'Invalid shared credential snapshot.'
flock -u 9
export_local || fail 'Could not export local credentials.'
canonical "$work/local.json" "$work/local.sorted" || fail 'Invalid local credential export.'
if ! cmp -s "$work/baseline.sorted" "$work/local.sorted"; then
    jq -r '.[].id | @uri' "$work/local.json" >"$work/ids"
    while IFS= read -r id; do
        timeout --foreground 30s "$binary" api DELETE "/api/credential/$id" >"$work/log" 2>&1 || fail 'Could not replace local credentials.'
    done <"$work/ids"
    # Native import skips existing IDs, so cached credentials must be removed.
    timeout --foreground 30s "$binary" auth import "$work/desired.json" >"$work/log" 2>&1 || fail 'Could not import shared credentials.'
    export_local && canonical "$work/local.json" "$work/local.sorted" || fail 'Could not verify imported credentials.'
    cmp -s "$work/baseline.sorted" "$work/local.sorted" || fail 'Imported credentials do not match shared auth.'
fi
sync -f "$work/baseline.sorted"
mv -- "$work" "$pending"
work=$pending
sync -f "$store"
write_back=true
"$binary" "$@" 9>&-
