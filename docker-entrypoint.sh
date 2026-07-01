#!/bin/sh
set -eu

if [ "$#" -eq 0 ]; then
    set -- /usr/local/bin/palpanel
elif [ "${1#-}" != "$1" ]; then
    set -- /usr/local/bin/palpanel "$@"
fi

PALPANEL_RUNTIME_UID="${PALPANEL_UID:-10001}"
PALPANEL_RUNTIME_GID="${PALPANEL_GID:-10001}"
PALPANEL_RUNTIME_HOME="${HOME:-/data}"
PALPANEL_STEAMCMD_STATE="${PALPANEL_STEAMCMD_DIR:-/data/steamcmd}"

if [ "$(id -u)" != "0" ]; then
    exec "$@"
fi

case "$PALPANEL_RUNTIME_UID" in
    "" | *[!0-9]*)
        echo "PALPANEL_UID must be numeric" >&2
        exit 1
        ;;
esac

case "$PALPANEL_RUNTIME_GID" in
    "" | *[!0-9]*)
        echo "PALPANEL_GID must be numeric" >&2
        exit 1
        ;;
esac

if [ "$(id -g palpanel)" != "$PALPANEL_RUNTIME_GID" ]; then
    groupmod -o -g "$PALPANEL_RUNTIME_GID" palpanel
fi

if [ "$(id -u palpanel)" != "$PALPANEL_RUNTIME_UID" ] || [ "$(id -g palpanel)" != "$PALPANEL_RUNTIME_GID" ]; then
    usermod -o -u "$PALPANEL_RUNTIME_UID" -g "$PALPANEL_RUNTIME_GID" -d "$PALPANEL_RUNTIME_HOME" palpanel
fi

mkdir -p /data /palserver "$PALPANEL_STEAMCMD_STATE"

if [ "${PALPANEL_FIX_OWNERSHIP:-true}" != "false" ]; then
    chown -R "$PALPANEL_RUNTIME_UID:$PALPANEL_RUNTIME_GID" /data /palserver "$PALPANEL_STEAMCMD_STATE"
fi

export HOME="$PALPANEL_RUNTIME_HOME"
exec gosu palpanel "$@"
