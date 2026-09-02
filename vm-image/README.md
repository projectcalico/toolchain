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
PROJECT=unique-caldron-775 ZONE=us-central1-a FAMILY=ci-base ./vm-image/build-image.sh
```

Runs from anywhere — it resolves the repo (and
`images/calico-go-build/versions.yaml`, see below) from its own path. ~3–4 min.
Re-run any time to pick up newer tool versions — because the image lives in a
**family**, `createvm` (which asks for the family) automatically gets the newest
one; no code change. Old images can be pruned later
(`gcloud compute images list --filter="family=ci-base"`).

You need `yq`, and gcloud authed as an identity with compute instance + image
create/delete in `PROJECT`.

## Go version and the go-build image

The VM's Go tracks **`images/calico-go-build/versions.yaml`**, not this repo's
`go.mod`. A job that runs on the VM is a job that would otherwise have run inside
`calico/go-build`, so it has to see the same Go — and that file is the single
source of truth the go-build image itself is built from.

`build-image.sh` resolves them with the same helper the release tagging uses and
injects them into `provision.sh` as a preamble (the provisioner runs as the
builder's startup-script, where it cannot read the repo):

| Injected | Source |
|---|---|
| `GO_VERSION` | `hack/generate-version-tag-name.sh -f images/calico-go-build/versions.yaml -g` |
| `GO_SHA256` | `.golang.checksum.sha256.amd64` — verified after download, as the go-build Dockerfile does |
| `GO_BUILD_IMAGE` | `calico/go-build:$(hack/generate-version-tag-name.sh -f images/calico-go-build/versions.yaml)` |

So bumping `versions.yaml` and re-running `build-image.sh` is all it takes to keep
the VM image and the go-build image in step. `provision.sh` **requires** these to
be set rather than defaulting them — silently baking a stale Go into the image is
the exact drift this indirection exists to prevent.

## Point createvm at it

`createvm` reads the image from env (defaults are stock Ubuntu):

```
GOOGLE_VM_IMAGE_PROJECT=unique-caldron-775
GOOGLE_VM_IMAGE_FAMILY=ci-base
```

These are already `createvm`'s defaults; override them to boot a stock-Ubuntu VM
instead. Jobs consuming this image can drop their own tool-install and
docker-bootstrap steps — the image already has docker, and the login user is
already in the docker group.

## Pre-baked docker images

`provision.sh` pre-pulls the heavy images into the image's docker cache so jobs
skip the pull: `kindest/node` (3 kind clusters), `calico/go-build` (the
`make build-calico-image` + operator builds), and `registry:2` (the pull-through
caches + the local helm registry). The `calico/go-build` tag is injected from
`versions.yaml` (above), so it cannot drift. `kindest/node` is still hardcoded in
`provision.sh` — it comes from calico's `lib/kind` `DefaultNodeImage` — so re-run
`build-image.sh` to refresh the image when that bumps.

## How it works (the four-step image recipe)

1. **Builder** — a normal VM from stock Ubuntu, running `provision.sh` at boot.
2. **Provision** — install everything; signal `/var/run/provision-done`.
3. **Snapshot** — clean-stop the builder, then `gcloud compute images create
   --source-disk=<builder-disk> --family=ci-base`.
4. **Discard** — delete the builder.

GCE regenerates per-instance state (SSH host keys, machine id) on first boot of
instances created from the image, so no manual "deprovision" step is needed for
this use.
