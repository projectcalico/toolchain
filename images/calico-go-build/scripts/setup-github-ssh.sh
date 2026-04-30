#! /bin/bash

# github.com host keys are pre-installed at /etc/ssh/ssh_known_hosts (see Dockerfile),
# so no runtime ssh-keyscan is needed.

mkdir -p ~/.keys

eval "$(ssh-agent -s)"
create-local-secret "marvin" "${HOME}/.keys/marvin"

chmod 0600 ~/.keys/*
ssh-add ~/.keys/*
