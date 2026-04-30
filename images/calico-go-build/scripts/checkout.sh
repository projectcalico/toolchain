#! /bin/bash

source setup-github-ssh

# clone repo and checkout branch if required
git clone ${CI_GIT_URL} ${CI_GIT_DIR}
cd ${CI_GIT_DIR}
if [ "${CI_GIT_CLONED_BRANCH}" != "" ]; then
    echo "[INFO] Checking out branch ${CI_GIT_CLONED_BRANCH}"
    git checkout ${CI_GIT_CLONED_BRANCH}
fi
if [ "${CI_GIT_SHA}" != "" ]; then
    echo "[INFO] Checking out commit ${CI_GIT_SHA}"
    git checkout ${CI_GIT_SHA}
else
    echo "[WARN] CI_GIT_SHA env var is empty - not running on exact commit"
fi
