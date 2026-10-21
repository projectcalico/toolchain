#  Copyright 2023 Google LLC
#
#  Licensed under the Apache License, Version 2.0 (the "License");
#  you may not use this file except in compliance with the License.
#  You may obtain a copy of the License at
#
#      http://www.apache.org/licenses/LICENSE-2.0
#
#  Unless required by applicable law or agreed to in writing, software
#  distributed under the License is distributed on an "AS IS" BASIS,
#  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#  See the License for the specific language governing permissions and
#  limitations under the License.

# Upstream checked exit status by hand and kept missing places: a failed image
# listing, a find inside a pipeline, a snapshot that never copied. The script
# signals success by printing a marker at the end, so each gap published a disk
# anyway. Strict mode inverts the default -- an unchecked failure now stops the
# script, and the commands that may legitimately fail are marked as such below.
set -euo pipefail

sudo apt update
sudo apt-get update

# Install containerd.
sudo apt install --yes containerd
# Configure docker.io registry mirror
sudo mkdir -p /etc/containerd/certs.d/docker.io
if [ ! -f /etc/containerd/certs.d/docker.io/hosts.toml ]; then
  sudo tee /etc/containerd/certs.d/docker.io/hosts.toml <<EOF
server = "https://registry-1.docker.io"

[host."https://mirror.gcr.io"]
  capabilities = ["pull", "resolve"]
EOF
else
  echo "The file /etc/containerd/certs.d/docker.io/hosts.toml already exists. Skipping..."
fi
# Start containerd.
sudo systemctl start containerd
# Check to see if containerd is up and we can use ctr.
if sudo ctr version | grep "Server:"; then
  echo containerd is ready to use
else
  echo containerd is not running. Please rerun the tool to try it again.
  exit 1
fi

# Check if disk is partitioned and update device node file path accordingly if so.
# The device name that maps to the `google-<device_name>` path is defined here: https://github.com/GoogleCloudPlatform/ai-on-gke/blob/4b73f02abd71e3c2a836d4d0ce29de054a605bc6/gke-disk-image-builder/imager.go#L32
# The disk name prefix is constructed here: https://github.com/GoogleCloudPlatform/ai-on-gke/blob/4b73f02abd71e3c2a836d4d0ce29de054a605bc6/gke-disk-image-builder/imager.go#L115
DEVICE_NODE=/dev/disk/by-id/google-secondary-disk-image-disk
if [[ -e "$DEVICE_NODE-part1" ]]; then
  DEVICE_NODE="$DEVICE_NODE-part1"
fi
echo "using device node: $DEVICE_NODE"

# Check if the device exists
if ! [ -b "$DEVICE_NODE" ]; then
  echo "Device $DEVICE_NODE does not exist. Please rerun the tool to try it again."
  exit 1
fi
# Set ext4 as the file system.
# `if !` rather than a $? test: under set -e the failure exits before the test is
# ever reached, taking the explanation with it. Same shape throughout this file.
if ! sudo mkfs.ext4 -F -m 0 -E lazy_itable_init=0,lazy_journal_init=0,discard "$DEVICE_NODE"; then
  echo Failed to create the filesystem on "$DEVICE_NODE". Please rerun the tool to try it again.
  exit 1
fi

# Install JQ
echo Installing JQ...
sudo apt-get --yes install jq

# Fetch and store the OAuth token of the service account in ACCESS_TOKEN.
echo Fetching OAuth token...
# Allowed to fail: it is only used by the serviceaccounttoken mechanism, and a
# `none` build must not die because the metadata server was briefly unavailable.
# The pull fails loudly later if the token was needed and is empty.
ACCESS_TOKEN=$(curl -sSf -H "Metadata-Flavor: Google" http://metadata.google.internal/computeMetadata/v1/instance/service-accounts/default/token | jq -r '.access_token' || true)

function remove_snapshot_views() {
  echo Removing the previously created snapshot views...
  # No views is the ordinary case on a fresh builder, and grep exits 1 on no match,
  # which pipefail would turn into a fatal error.
  views=($(sudo ctr -n k8s.io snapshot list | grep "View" | sed 's/ \{1,\}/,/g' || true))
  for row in "${views[@]}"; do
    view=$(echo "$row" | cut -d',' -f1)
    echo Removing the view $view
    sudo ctr -n k8s.io snapshot rm "$view" 2>/dev/null || true
  done
}

function pull_images() {
  echo Pulling the given container images...
  for param in "$@"; do
    echo Start pulling $param ...
    pulled=true
    if [ "$OAUTH_MECHANISM" == "none" ]; then
      sudo ctr -n k8s.io image pull --hosts-dir "/etc/containerd/certs.d" "$param" || pulled=false
    elif [ "$OAUTH_MECHANISM" == "serviceaccounttoken" ]; then
      sudo ctr -n k8s.io image pull --hosts-dir "/etc/containerd/certs.d" --user "oauth2accesstoken:$ACCESS_TOKEN" "$param" || pulled=false
    else
      echo "Unknown OAuth mechanism, expected 'None' or 'ServiceAccountToken' but got '$OAUTH_MECHANISM'".
      exit 1
    fi
    if [ "$pulled" != true ]; then
      echo Failed to pull and unpack the image "$param". Please rerun the tool to try it again.
      exit 1
    fi
  done
}

function write_image_info() {
  echo Writing image info to disk image...
  # Both checked: an unchecked listing wrote a bare newline and returned success, and
  # the caller does not test this function's status, so the disk published without
  # its image inventory.
  if ! images=$(sudo ctr -n k8s.io images ls); then
    echo Failed to list the pulled images. Please rerun the tool to try it again.
    exit 1
  fi
  if ! echo "$images" >> "/mnt/disks/container_layers/images.metadata"; then
    echo Failed to write the image inventory. Please rerun the tool to try it again.
    exit 1
  fi
}

function process_snapshots() {
  echo Processing the snapshots...
  # Both guards matter because this script has no `set -e` and ends by printing the
  # marker Daisy treats as success: a listing that failed, or one with nothing
  # committed, would skip the loop entirely and still publish an empty disk.
  if ! snapshot_list=$(sudo ctr -n k8s.io snapshot list); then
    echo "Failed to list containerd snapshots. Please rerun the tool to try it again."
    exit 1
  fi
  # grep exits 1 on no match, which pipefail would make fatal before the explicit
  # check below could explain what happened.
  snapshots=($(echo "$snapshot_list" | grep "Committed" | cut -d ' ' -f1 || true))
  if [ ${#snapshots[@]} -eq 0 ]; then
    echo "No committed snapshots to unpack: nothing was pulled, so there is no cache to publish."
    exit 1
  fi

  for snapshot in "${snapshots[@]}"; do
    echo Processing $snapshot
    # Cleared per snapshot: left set, a snapshot whose every retry failed would reuse
    # the previous one's path and record this chain ID against the wrong layer.
    original_path=""

    # Add retry in case `ctr snapshot view` fails silently
    local retries=5
    while [ ${retries} -ge 1 ]; do
      # ((retries--)) is safe as written, since the loop guard keeps the pre-decrement
      # value non-zero, but it returns that value as its status and so is one edit to
      # the guard away from killing the loop under set -e. This form always returns 0.
      retries=$((retries - 1))
      # Every failure in this loop is a retry, not a fatal: it exists because the
      # view sometimes fails silently. Each one is therefore handled explicitly
      # rather than left to set -e.
      if ! sudo ctr -n k8s.io snapshot view "tmp_$snapshot" "$snapshot"; then
        echo "Failed to create snapshot view for $snapshot. Will retry. ${retries} retries left."
        continue
      fi

      # grep finds nothing when the mount did not materialise, and head closes the
      # pipe early; either makes the pipeline non-zero, which is a retry here.
      original_path=$(sudo ctr -n k8s.io snapshot mount "/tmp_$snapshot" "tmp_$snapshot" | grep -oP '/\S+/snapshots/[0-9]+/fs' | tr ':' '\n' | head -n 1 || true)
      if [[ -n "$original_path" ]]; then
        break
      fi
      echo "Failed to get mount point for tmp_$snapshot. Will retry. ${retries} retries left."
      echo "All snapshots:"
      sudo ctr -n k8s.io snapshot list || true
      sudo ctr -n k8s.io snapshot rm "tmp_$snapshot" || true
      sleep 1
    done

    if [[ -z "$original_path" ]]; then
      echo Failed to get snapshot directory for snapshot $snapshot. Please rerun the tool to try it again.
      exit 1
    fi

    new_path=$(echo "$original_path" | grep -o "snapshots/.*/fs" || true)
    if [ -z "$new_path" ]; then
      echo Could not derive a layer path from "$original_path" for "$snapshot". Please rerun the tool to try it again.
      exit 1
    fi
    sudo mkdir -p "/mnt/disks/container_layers/${new_path}"
    # -a, not -r -p: capabilities and overlayfs opaque markers live in extended
    # attributes, which -p does not copy and the content-only checksums cannot see,
    # so a layer would cache with different behaviour and still verify.
    if ! sudo cp -a "$original_path" "/mnt/disks/container_layers/${new_path}/.."; then
      echo Failed to copy the snapshot files for "$snapshot" from "$original_path" to /mnt/disks/container_layers/"${new_path}". Please rerun the tool to try it again.
      exit 1
    fi

    mapping="$snapshot $new_path"

    if [ "$STORE_SNAPSHOT_CHECKSUMS" = "true" ]; then
      echo "Calculating checksum for snapshot $snapshot"
      # pipefail matters here: a find that failed used to hash an empty stream and
      # record the md5 of nothing, which verify.sh would then match against a
      # missing directory.
      checksum="$(find "/mnt/disks/container_layers/${new_path}" -type f -exec md5sum {} + | cut -d' ' -f1 | LC_ALL=C sort | md5sum | cut -d' ' -f1)"
      mapping="$snapshot $new_path $checksum"
    fi

    echo "Appending $mapping to Metadata file"
    if ! echo "$mapping" >> "/mnt/disks/container_layers/snapshots.metadata"; then
      echo Failed to write metadata view for "$snapshot". Please rerun the tool to try it again.
      exit 1
    fi
  done
  echo Processing is done.
}

function unpack() {
  # Prepare the disk image directories.
  echo Preparing the disk image directories...
  sudo mkdir -p /mnt/disks/container_layers
  if ! sudo mount -o discard,defaults "$DEVICE_NODE" /mnt/disks/container_layers; then
    echo Failed to mount "$DEVICE_NODE". Please rerun the tool to try it again.
    exit 1
  fi
  sudo chmod a+w /mnt/disks/container_layers
  # -f: the disk was just mkfs'd, so this file does not exist yet and a bare rm
  # would now be fatal.
  sudo rm -f /mnt/disks/container_layers/snapshots.metadata
  sudo touch /mnt/disks/container_layers/snapshots.metadata
  sudo chmod a+w /mnt/disks/container_layers/snapshots.metadata

  # Remove the previously created snapshot views.
  remove_snapshot_views

  # Store the first parameter in STORE_SNAPSHOT_CHECKSUMS and shift
  STORE_SNAPSHOT_CHECKSUMS=$(echo "$1")
  shift

  # Store the second parameter in OAUTH_MECHANISM and shift.
  OAUTH_MECHANISM=$(echo "$1" | tr '[:upper:]' '[:lower:]')
  shift

  # Pull all the given images.
  pull_images "$@"

  # Write image info to disk image.
  write_image_info "$@"

  # Process the snapshots.
  process_snapshots

  # Remove the original pulled images.
  for img in "${@}"; do
    echo Removing the original pulled image $img ...
    sudo ctr -n k8s.io image rm "$img" || true
  done

  echo Content of snapshots.metadata file:
  sudo cat /mnt/disks/container_layers/snapshots.metadata

  # We want to unmount the directory, otherwise we don't be able to generate the
  # image properly.
  # Checked: still mounted means writes may not have reached the disk, and the
  # marker below would have Daisy image it anyway.
  if ! sudo umount /mnt/disks/container_layers; then
    echo "Failed to unmount /mnt/disks/container_layers. Please rerun the tool to try it again."
    exit 1
  fi

  # This script must print out this message as a signal to the caller script,
  # meaning the unpacking is completed and the disk image creation can begin.
  echo Unpacking is completed.
}
