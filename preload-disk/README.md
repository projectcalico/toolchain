# GKE secondary-boot-disk image preloading

A GCE **disk image** with heavy container images baked in, attached to a GKE node
pool as a **secondary boot disk** so pods start with those images already on the
node — no pull. Built for the argoci build pools: preloading `calico/go-build`
removes the multi-hundred-MB go-build pull that every kind-rig `build-artifacts`
run (and any other go-build CI step) otherwise pays.

This is the GKE-native equivalent of the raw-VM [`vm-image`](../vm-image) baking:
you can't give a managed node pool a custom OS image, but you *can* preload
container images onto it via a secondary disk.

## Files

- **`build-preload-disk.sh`** — wraps Google's `gke-disk-image-builder`
  (`github.com/ai-on-gke/tools`, `gke-disk-image-builder`):
  it spins up a throwaway builder VM, pulls the images onto a data disk in the
  containerd image-streaming layout, snapshots that disk into a GCE image, and
  deletes the builder. Prints the `gcloud` line to attach the result.
- **`versions.yaml`** — the upstream commit the builder is fetched at.

### Why fetch it instead of importing it

`gke-disk-image-builder` has a perfectly good library API (`imager.Request` /
`imager.GenerateDiskImage`), but it cannot be added to `go.mod`: the module's
`go.mod` still declares `github.com/GoogleCloudPlatform/ai-on-gke/gke-disk-image-builder`
while the code now lives at `github.com/ai-on-gke/tools`, so `go get` at the real
location fails on a module-path mismatch, and the declared path resolves only to a
2023 snapshot whose source directory has since been deleted from that repo.

Fetching a pinned commit gets current code and keeps `compute-daisy` and the
`cloud.google.com/go` stack — about 17 extra modules — out of this repo's
dependency graph, which matters because `scratch-utils` shares it.

## Build

```bash
cd preload-disk
PROJECT=<cluster-project> ZONE=us-central1-a \
  GCS_PATH=gs://<log-bucket> ./build-preload-disk.sh
```

~5–8 min. Knobs (env):

| var | default | meaning |
|---|---|---|
| `PROJECT` | `unique-caldron-775` | where the image (and builder VM) are created — **must be the node pool's project** |
| `ZONE` | `us-central1-a` | builder VM zone |
| `GCS_PATH` | *(required)* | `gs://` bucket/path for builder logs |
| `IMAGE_NAME` | `go-build-preload-<go-build tag>` | Named off the go-build release tag, as the go-build images are, so the disk and the image it preloads match by eye. Dots become hyphens (GCE names are RFC1035). |
| `CONTAINER_IMAGES` | the argoci `calico/go-build` tag | space-separated refs to preload (each needs a tag or digest) |
| `DISK_SIZE_GB` | `20` | data-disk size (must hold every preloaded image) |
| `NETWORK` / `SUBNET` | `semaphore-autotest` | Builder VM network. Not `default`: that network is legacy in this project (no subnets), and the builder always requests a subnetwork. |
| `AI_ON_GKE_REF` | pinned in [`versions.yaml`](versions.yaml) | ai-on-gke/tools commit the builder is fetched at. A branch or tag name also works, for testing an upstream change. |

Needs gcloud authed with compute instance/disk/image create+delete and log-bucket
write in `PROJECT`, a local Go toolchain (`go run ./cli`), and git.

## Attach to a node pool

Secondary boot disk is set at node-pool **create** time (not an in-place update),
so make a new pool (or recreate) referencing the image the build printed:

```bash
gcloud container node-pools create <pool> \
  --cluster=<cluster> --location=<loc> \
  --enable-image-streaming \
  --secondary-boot-disk=disk-image=projects/<PROJECT>/global/images/<IMAGE_NAME>,mode=CONTAINER_IMAGE_CACHE
```

For the kind-rig build pool, keep the `large` preset's labels/taints so
`build-artifacts` lands there (see the argoci `jobSizePresets`):
`--node-labels=role=argoci,size=large`
`--node-taints=argoci=:NoSchedule,argoci-nonspot=:NoSchedule,argoci-large=:NoSchedule`.

Verify a scheduled pod's image shows as already-present (its `kubectl describe pod`
event is not `Pulling` for seconds).

## Requirements & caveats

- **GKE version:** ≥ `1.30.1-gke.1329000` (COS_CONTAINERD) or ≥ `1.35.0-gke.1403000`
  (UBUNTU_CONTAINERD). **Image streaming must be enabled** on the pool
  (`--enable-image-streaming`); COS/Ubuntu containerd node images only.
- **Digest match:** the cache hits only the exact ref a pod requests. `calico/go-build`
  is pinned (high-value, stable). A moving tag (`:master`) drifts and stops hitting —
  preload only stable refs, or rebuild on a schedule.
- **Refresh:** GCE images are immutable and a pool binds one at create time. When
  the go-build tag bumps, rebuild (new `IMAGE_NAME`) and recreate/roll the pool.
  Keep `CONTAINER_IMAGES` in sync with the pod image (calico `metadata.mk`
  `GO_BUILD_VER`).
- **Project:** the image must live in the cluster's project (or be shared to it).
  Default `PROJECT` is where the kind-rig CI VMs/secrets live — if the argoci
  cluster is in a different project, set `PROJECT` to that.
- Preloads container images only — not host binaries or an OS. For host tooling on
  a raw CI VM, that's [`vm-image`](../vm-image).

## Automating it

Like `vm-image`, this is a script today; a Semaphore block + promotion (build the
disk image on merge to master / release branches, as for the scratch-utils/ci-base
images) can be layered on later.
