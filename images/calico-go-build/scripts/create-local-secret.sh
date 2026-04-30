#! /bin/bash

# Restrict permissions on any file/dir we create so the secret is never briefly
# world-readable between write and chmod.
umask 077

__secretname=$1
__secretpath=$2
if printenv "${__secretname}" > /dev/null; then
    mkdir -p "$(dirname "${__secretpath}")"
    printenv "${__secretname}" > "${__secretpath}"
    echo "Created secret file at ${__secretpath}"
else
    echo "Secret ${__secretname} not created as env var not found"
fi
