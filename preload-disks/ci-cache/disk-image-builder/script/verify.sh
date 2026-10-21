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

# Upstream relied on every command being checked by hand, and repeatedly was not:
# an unchecked mount, a find that failed inside a pipeline, a variable only set on
# one branch. The script signals success by printing a marker, so each gap published
# a disk. Strict mode makes an unchecked failure stop the script by default; the
# places that may legitimately fail say so explicitly below.
set -euo pipefail

# Daisy watches the serial console for one of two strings and waits out its timeout if
# it sees neither. Strict mode exits silently on any unchecked failure, so without
# this the VM would sit there until the deadline -- `apt update` returning 100 is
# enough. The trap guarantees the marker and preserves the status.
#
# "verfication" is upstream's spelling in imager.go's FailureMatch. It has to match.
trap 'rc=$?; [ "$rc" -eq 0 ] || echo "Image verfication failure: the verification script exited with status $rc."; exit "$rc"' EXIT

sudo apt update
sudo apt-get update

# Install containerd.
sudo apt install --yes containerd
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
  echo "Image verfication failure: failed to get device: Device $DEVICE_NODE does not exist. Please rerun the tool to try it again."
  exit 1
fi

function verify_snapshots() {
  # Only ever set on the mismatch branch, so under `set -u` the check at the end
  # would be an unbound-variable error on every clean run.
  snapshot_broken=""

  # Prepare the disk image directories.
  echo Preparing the disk image directories...
  sudo mkdir -p /mnt/disks/container_layers
  if ! sudo mount -o discard,defaults "$DEVICE_NODE" /mnt/disks/container_layers; then
    echo "Image verfication failure: failed to mount $DEVICE_NODE. Please rerun the tool to try it again."
    exit 1
  fi

  echo verifying the snapshots...
  sudo ls /mnt/disks/container_layers
  snapshot_metadata_file="/mnt/disks/container_layers/snapshots.metadata"
  # Missing or empty means zero iterations below, and with no `set -e` the script
  # then prints success: an empty disk or a failed mount certified as verified.
  if [ ! -s "$snapshot_metadata_file" ]; then
    echo "Image verfication failure: $snapshot_metadata_file is missing or empty; nothing was verified."
    exit 1
  fi
  while IFS= read -r line
  do
    echo "$line"
    snapshot_chainID=$(echo $line | cut -d' ' -f1)
    snapshot_path=$(echo $line | cut -d' ' -f2)
    expected_checksum=$(echo $line | cut -d' ' -f3)
    
    if [ -z "$expected_checksum" ]; then
      echo "Image verfication failure: Expected checksums not found in snapshots.metadata. Please use --store-snapshot-checksum flag when building images."
      exit 1
    fi

    # A missing directory hashes to the md5 of nothing, which is exactly the
    # checksum recorded for a layer that legitimately has no regular files -- so a
    # layer that never reached the disk verified clean. `find` does report the
    # failure, but before pipefail the pipeline swallowed it.
    snapshot_dir="/mnt/disks/container_layers/${snapshot_path}"
    if [ ! -d "$snapshot_dir" ]; then
      echo "Image verfication failure: snapshot directory $snapshot_dir is missing for $snapshot_chainID."
      exit 1
    fi
    actual_checksum="$(find "$snapshot_dir" -type f -exec md5sum {} + | cut -d' ' -f1 | LC_ALL=C sort | md5sum | cut -d' ' -f1)"

    if [ "$expected_checksum" = "$actual_checksum" ]; then
      echo "Verification succeeds for snapshot $snapshot_chainID at $snapshot_path."
    else
      echo "Verification fails for snapshot $snapshot_chainID at $snapshot_path. Expected checksum: $expected_checksum, got: $actual_checksum"
      snapshot_broken="true"
    fi
  done < "$snapshot_metadata_file"

  if [ -n "$snapshot_broken" ]; then
    echo "Image verfication failure: Snapshot checksum mismatch. Please see the log to find more details."
  else
    echo "Disk image verification succeeds."
  fi
}
