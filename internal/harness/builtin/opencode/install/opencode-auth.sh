#!/bin/bash
# OpenCode owns its backend and credential format. Shared auth is updated only
# when it still matches the snapshot the first active client imported.
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
[[ ! -L $store/lock && ! -L $store/gate ]] || fail 'Invalid local auth lock.'
# The gate serializes first/last-client transitions. Each foreground client
# holds a shared store lock, so imports and write-back require an idle store.
exec 7>"$store/gate"
exec 8>"$store/lock"
exec 9>"$shared/lock"
snapshot=$shared/credentials.json
pending=$store/pending
work=
config_temp=
write_back=false
keep=false

canonical() { jq -S 'sort_by(.id)' "$1" >"$2" 2>"$work/log"; }

configure_backend_port() {
    local file=$1 port=$2
    [[ ! -L $file && ( ! -e $file || -f $file ) ]] || return 1
    config_temp=$(mktemp "${file%/*}/.devbox-service.XXXXXX") || return 1
    if [[ -e $file ]]; then
        jq -e --argjson port "$port" 'if type == "object" then .port = $port else error("Invalid service configuration") end' "$file" >"$config_temp" || return 1
    else
        printf '{"port":%s}\n' "$port" >"$config_temp" || return 1
    fi
    mv -fT -- "$config_temp" "$file" || return 1
    config_temp=
}

prepare_backend() {
    local disabled endpoint config_dir service_config port configured attempt status
    disabled=$(timeout --foreground 30s "$binary" service get disabled 2>"$work/log") || fail 'Could not inspect local backend settings.'
    case $disabled in
        true) return ;;
        false) ;;
        *) fail 'Invalid local backend settings.' ;;
    esac
    endpoint=$(timeout --foreground 30s "$binary" service status 2>"$work/log") || fail 'Could not inspect the local backend.'
    case $endpoint in
        http://*|https://*) return ;;
        stopped) ;;
        *) fail 'Invalid local backend status.' ;;
    esac
    config_dir=$(timeout --foreground 30s "$binary" debug paths config 2>"$work/log") || fail 'Could not locate local backend settings.'
    [[ $config_dir == /* && -d $config_dir ]] || fail 'Invalid local backend settings directory.'
    service_config=$config_dir/service.json
    # Separate containers cannot share the native default port with host networking.
    # Keep native service configuration/discovery, but select a port at each cold start.
    for attempt in 1 2 3; do
        port=$(timeout --foreground 5s node -e '
const server = require("node:net").createServer();
server.once("error", () => process.exit(1));
server.listen(0, "127.0.0.1", () => {
    console.log(server.address().port);
    server.close();
});
' 2>"$work/log") || fail 'Could not select a local backend port.'
        [[ $port =~ ^[1-9][0-9]{0,4}$ && $port -le 65535 ]] || fail 'Invalid local backend port.'
        # The native setter stops the registered PID even when its registration is
        # stale. Update only the config field, never registration or process state.
        configure_backend_port "$service_config" "$port" 2>"$work/log" || fail 'Could not configure the local backend port.'
        configured=$(timeout --foreground 30s "$binary" service get port 2>"$work/log") || fail 'Could not verify the local backend port.'
        [[ $configured == "$port" ]] || fail 'Native backend settings rejected the selected port.'
        if timeout --foreground 30s "$binary" service start >"$work/log" 2>&1; then
            return
        else
            status=$?
        fi
        [[ $status != 124 ]] || fail 'Local backend startup timed out.'
        # Releasing the probe socket leaves a bind race. Only OpenCode's specific
        # bind-collision diagnostic permits retry, never arbitrary startup failures.
        if [[ $status != 1 ]] || ! grep -Eq "Managed service port $port on .* is already in use by " "$work/log"; then
            fail "Could not start the local backend (exit $status)."
        fi
    done
    fail 'Could not start the local backend after three port collisions.'
}

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
        keep=true
        if flock -w 30 7; then
            # Close rather than unlock: an orphaned foreground child must retain
            # its shared lock even if its wrapper was killed.
            exec 8>&-
            exec 8>"$store/lock"
            if flock -n -E 75 -x 8; then
                result=0
                commit || result=$?
                if [[ $result != 1 ]]; then
                    keep=false
                else
                    printf 'OpenCode auth write-back failed. Reopen this session to retry.\n' >&2
                fi
                [[ $result == 0 || $status != 0 ]] || status=1
            elif [[ $? != 75 ]]; then
                printf 'OpenCode auth could not lock write-back. Reopen this session to retry.\n' >&2
                [[ $status != 0 ]] || status=1
            fi
        else
            printf 'OpenCode auth could not lock client cleanup. Reopen this session to retry.\n' >&2
            [[ $status != 0 ]] || status=1
        fi
    fi
    if [[ -n $config_temp ]] && ! rm -f -- "$config_temp"; then
        printf 'OpenCode backend settings temporary-file cleanup failed.\n' >&2
        [[ $status != 0 ]] || status=1
    fi
    if ! $keep && [[ -n $work ]] && ! rm -rf -- "$work"; then
        printf 'OpenCode auth temporary-file cleanup failed.\n' >&2
        [[ $status != 0 ]] || status=1
    fi
    exit "$status"
}
trap finish EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

valid_pending() {
    [[ -d $pending && ! -L $pending && -f $pending/baseline.sorted && ! -L $pending/baseline.sorted ]]
}

initialize() {
    work=$(mktemp -d "$store/.sync.XXXXXX")
    prepare_backend
    # An interrupted group keeps its baseline with this backing store. Recovery
    # follows the same conflict rule, and only runs once no client holds the store.
    if [[ -e $pending || -L $pending ]]; then
        valid_pending || fail 'Invalid pending auth state.'
        rm -rf -- "$work"
        keep=true
        work=$pending
        result=0
        commit || result=$?
        if [[ $result == 1 ]]; then
            fail 'Could not recover interrupted write-back. Retry this session.'
        fi
        rm -rf -- "$work"
        work=$(mktemp -d "$store/.sync.XXXXXX")
        keep=false
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
    work=$pending write_back=true
    sync -f "$store"
}

flock -w 30 7 || fail 'Could not lock client startup.'
if flock -n -E 75 -x 8; then
    initialize
elif [[ $? == 75 ]]; then
    flock -s 8 || fail 'Could not join active clients.'
    valid_pending || fail 'Invalid active auth state.'
    work=$pending write_back=true
else
    fail 'Could not lock local auth.'
fi
flock -s 8 || fail 'Could not hold active client lock.'
flock -u 7
"$binary" "$@" 7>&- 9>&-
