#!/bin/sh
# TEX Share container entrypoint.
#
# The container starts as root only long enough to make the data volume
# writable by the unprivileged runtime user (bind mounts are frequently
# root-owned), then drops privileges and hands control to the server.
set -e

DATA_DIR="${TEXSHARE_DATA_DIR:-/data}"

if [ "$(id -u)" = "0" ]; then
    mkdir -p "$DATA_DIR"
    chown -R texshare:texshare "$DATA_DIR" 2>/dev/null || true
    exec su-exec texshare:texshare /usr/local/bin/texshare "$@"
fi

exec /usr/local/bin/texshare "$@"
