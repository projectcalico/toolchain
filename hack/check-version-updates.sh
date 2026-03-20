#!/bin/bash

set -eu

ver_file=""

while getopts ":f:" opt; do
    case $opt in
    f)
        ver_file="$OPTARG"
        ;;
    :)
        echo "option: -$OPTARG requires an argument" >&2
        exit 1
        ;;
    *)
        echo "invalid argument -$OPTARG" >&2
        exit 1
        ;;
    esac
done

if [[ -z "$ver_file" ]]; then
    echo "usage: $0 -f <versions.yaml>" >&2
    exit 1
fi

updates=""

# --- Go update ---

current_go_ver=$(yq -r .golang.version "$ver_file")
go_minor="${current_go_ver%.*}"

echo "Checking Go updates for ${go_minor}.x series (current: ${current_go_ver})..."

go_api_response=$(curl -sf "https://go.dev/dl/?mode=json&include=all")

# Find latest stable release in the current minor series (sort numerically by patch version)
latest_go_ver=$(echo "$go_api_response" | jq -r --arg minor "go${go_minor}" \
    '[.[] | select(.stable == true) | select(.version | startswith($minor + "."))]
     | sort_by(.version | ltrimstr("go") | split(".") | map(tonumber))
     | last | .version // empty' \
)

if [[ -z "$latest_go_ver" ]]; then
    echo "No stable Go release found for ${go_minor}.x series"
else
    # Strip "go" prefix: go1.24.14 -> 1.24.14
    latest_go_ver="${latest_go_ver#go}"

    if [[ "$latest_go_ver" != "$current_go_ver" ]]; then
        echo "Go update available: ${current_go_ver} -> ${latest_go_ver}"

        # Update version
        yq -i ".golang.version = \"${latest_go_ver}\"" "$ver_file"

        # Update checksums for each architecture
        for arch in amd64 arm64 ppc64le s390x; do
            sha256=$(echo "$go_api_response" | jq -r --arg ver "go${latest_go_ver}" --arg arch "$arch" \
                '.[] | select(.version == $ver) | .files[] | select(.os == "linux" and .arch == $arch and .kind == "archive") | .sha256' \
            )
            if [[ -z "$sha256" ]]; then
                echo "ERROR: Could not find SHA256 for go${latest_go_ver} linux/${arch}" >&2
                exit 1
            fi
            yq -i ".golang.checksum.sha256.${arch} = \"${sha256}\"" "$ver_file"
            echo "  ${arch}: ${sha256}"
        done

        updates="${updates}Go: ${current_go_ver} -> ${latest_go_ver}\n"
    else
        echo "Go is up to date (${current_go_ver})"
    fi
fi

# --- Kubernetes update ---

current_k8s_ver=$(yq -r .kubernetes.version "$ver_file")
k8s_minor="${current_k8s_ver%.*}"

echo "Checking Kubernetes updates for ${k8s_minor}.x series (current: ${current_k8s_ver})..."

latest_k8s_ver=$(curl -sf "https://dl.k8s.io/release/stable-${k8s_minor}.txt")

if [[ -z "$latest_k8s_ver" ]]; then
    echo "Could not fetch latest Kubernetes ${k8s_minor}.x version"
else
    # Strip "v" prefix: v1.33.9 -> 1.33.9
    latest_k8s_ver="${latest_k8s_ver#v}"

    if [[ "$latest_k8s_ver" != "$current_k8s_ver" ]]; then
        echo "Kubernetes update available: ${current_k8s_ver} -> ${latest_k8s_ver}"

        yq -i ".kubernetes.version = \"${latest_k8s_ver}\"" "$ver_file"

        updates="${updates}Kubernetes: ${current_k8s_ver} -> ${latest_k8s_ver}\n"
    else
        echo "Kubernetes is up to date (${current_k8s_ver})"
    fi
fi

# --- Summary ---

if [[ -n "$updates" ]]; then
    echo ""
    echo "UPDATES_SUMMARY_START"
    echo -e "$updates"
    echo "UPDATES_SUMMARY_END"
fi
