# CI VM base image

A custom GCE image (family **`ci-base`**) with the common CI toolchain baked in —
docker, go, kind, kubectl, gh, git/make/curl, and the inotify sysctls — for **any**
CI job that runs on a GCE VM, not just the kind rig. `createvm` boots VMs from it,
so a job does **zero** per-boot installs (the ~1–2 min those take moves to
image-build time, once).

## Files

- **`provision.sh`** — installs the toolchain. Runs once, as the builder VM's
  startup-script. Publishes `/var/run/provision-done` when finished.
- **`build-image.sh`** — creates a throwaway builder, runs `provision.sh`,
  snapshots its disk into the `ci-base` family, deletes the builder.

## Build / refresh the image

```bash
cd cloud/kind-rig/vm-image
PROJECT=unique-caldron-775 ZONE=us-central1-a FAMILY=ci-base ./build-image.sh
```

~3–4 min. Re-run any time to pick up newer tool versions — because the image
lives in a **family**, `createvm` (which asks for the family) automatically gets
the newest one; no code change. Old images can be pruned later
(`gcloud compute images list --filter="family=ci-base"`).

You need gcloud authed as an identity with compute instance + image
create/delete in `PROJECT`.

## Point createvm at it

`createvm` reads the image from env (defaults are stock Ubuntu):

```
GOOGLE_VM_IMAGE_PROJECT=unique-caldron-775
GOOGLE_VM_IMAGE_FAMILY=ci-base
```

Once the image is proven, flip those to `createvm`'s defaults and **strip the
tool-install block out of `run-kindrig.sh`** (it becomes just "run the rig"), and
drop the docker-only `vm-bootstrap.sh` entirely — the image already has docker
and the user's already in the docker group.

## Pre-baked docker images

`provision.sh` pre-pulls the heavy images into the image's docker cache so jobs
skip the pull: `kindest/node` (3 kind clusters), `calico/go-build` (the
`make build-calico-image` + operator builds), and `registry:2` (the pull-through
caches + the local helm registry). POC: the tags are hardcoded in `provision.sh`
— `kindest/node` from `lib/kind` `DefaultNodeImage`, `calico/go-build` from
`metadata.mk` `GO_BUILD_VER`. They pin versions, so re-run `build-image.sh` to
refresh the image when those bump.

## How it works (the four-step image recipe)

1. **Builder** — a normal VM from stock Ubuntu, running `provision.sh` at boot.
2. **Provision** — install everything; signal `/var/run/provision-done`.
3. **Snapshot** — clean-stop the builder, then `gcloud compute images create
   --source-disk=<builder-disk> --family=ci-base`.
4. **Discard** — delete the builder.

GCE regenerates per-instance state (SSH host keys, machine id) on first boot of
instances created from the image, so no manual "deprovision" step is needed for
this use.
