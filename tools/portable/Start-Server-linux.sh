#!/bin/sh
# Shared Linux entry point. Runtime needs only /bin/sh and coreutils.
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$ROOT"
ARCH=${1:-}
case "$ARCH" in amd64|arm64) shift ;; *) echo 'Unsupported server architecture.' >&2; exit 2 ;; esac

usage() {
    echo "Usage: sh Start-Server-linux-$ARCH.sh <server IPv4> [port] [--dry-run]"
    echo 'Example: sh Start-Server-linux-amd64.sh 192.168.2.149 26020'
    echo 'Admin stays on 127.0.0.1 at port + 2. Stop with Ctrl+C or SIGTERM.'
}
if [ "$#" -eq 0 ]; then usage; exit 2; fi
if [ "$1" = '--help' ]; then usage; exit 0; fi
ADVERTISE_HOST=$1
shift
PORT=26020
if [ "$#" -gt 0 ] && [ "$1" != '--dry-run' ]; then PORT=$1; shift; fi
DRY_RUN=false
if [ "$#" -gt 0 ] && [ "$1" = '--dry-run' ]; then DRY_RUN=true; shift; fi
if [ "$#" -ne 0 ]; then usage >&2; exit 2; fi

case "$ADVERTISE_HOST" in
    ''|*[!0-9.]*|.*|*.|*..*) echo 'Server address must be an IPv4 address.' >&2; exit 2 ;;
esac
OLD_IFS=$IFS
IFS=.
set -- $ADVERTISE_HOST
IFS=$OLD_IFS
if [ "$#" -ne 4 ]; then echo 'Server address must have four IPv4 octets.' >&2; exit 2; fi
FIRST_OCTET=$1
for octet do
    if [ "${#octet}" -gt 3 ] || [ "$octet" -gt 255 ]; then echo 'Invalid IPv4 octet.' >&2; exit 2; fi
    case "$octet" in 0?*) echo 'IPv4 octets must not have leading zeros.' >&2; exit 2 ;; esac
done
if [ "$FIRST_OCTET" -eq 0 ] || [ "$FIRST_OCTET" -ge 224 ]; then
    echo 'Server address must be a unicast IPv4 address.' >&2; exit 2
fi
case "$PORT" in ''|*[!0-9]*|0?*) echo 'Port must be an integer from 1 to 65533.' >&2; exit 2 ;; esac
if [ "${#PORT}" -gt 5 ] || [ "$PORT" -lt 1 ] || [ "$PORT" -gt 65533 ]; then
    echo 'Port must be from 1 to 65533.' >&2; exit 2
fi
BATTLE_PORT=$((PORT + 1))
ADMIN_PORT=$((PORT + 2))

if [ "$(uname -s)" != Linux ]; then echo 'This entry point requires Linux.' >&2; exit 2; fi
case "$ARCH:$(uname -m)" in
    amd64:x86_64|amd64:amd64|arm64:aarch64|arm64:arm64) ;;
    *) echo "Wrong architecture: requested $ARCH, detected $(uname -m)." >&2; exit 2 ;;
esac
SERVER="$ROOT/kairi-server-linux-$ARCH"
if [ ! -f "$SERVER" ] || [ ! -f server-arguments.sh ]; then
    echo 'Incomplete package. Extract all release files first.' >&2; exit 1
fi
. "$ROOT/server-arguments.sh"
if [ "$DRY_RUN" = true ]; then
    echo 'Dry run: no server started and no account data written.'
    printf '%s\n' "$SERVER" "$@"
    exit 0
fi
mkdir -p "$ROOT/_local/data" "$ROOT/_local/runtime"
# The request recorder appends to an existing file. Preserve previous requests.
: >> "$ROOT/_local/runtime/requests.jsonl"
chmod u+x "$SERVER"
echo "Starting Linux $ARCH server at http://$ADVERTISE_HOST:$PORT"
echo "Admin: http://127.0.0.1:$ADMIN_PORT/ ; Ctrl+C/SIGTERM stops gracefully."
exec "$SERVER" "$@"
