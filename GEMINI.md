# Gemini Code Understanding for Project Calico Toolchain

This document provides a comprehensive overview of the Project Calico toolchain repository, designed to be understood by the Gemini family of models.

## Project Overview

This repository contains the necessary Docker-based toolchain images for building various components of [Project Calico](https://projectcalico.org). It includes environments for Go, LLVM, and Rust, along with other utilities required for the build and CI/CD processes.

The primary purpose of this repository is to provide consistent and versioned build environments to ensure reproducible builds of Calico components across different architectures.

## File Structure

The repository is organized as follows:

- `.github/workflows/`: Contains GitHub Actions workflows for automating tasks like branch and tag creation on version changes.
- `.semaphore/`: Contains the Semaphore CI configuration for building, testing, and publishing the toolchain images.
- `cmd/`: Contains Go source code for internal tools.
  - `binfmt/`: A tool to register and unregister `binfmt_misc` handlers for multi-architecture support.
  - `semvalidator/`: A tool to validate Semaphore CI configurations.
- `hack/`: Contains shell scripts used in the CI/CD process, such as generating version tags and branch names.
- `images/`: Contains the Dockerfiles and related configurations for building the toolchain images.
- `Makefile` / `lib.Makefile` / `Makefile.common`: Makefiles to automate the build process of the Docker images and other development tasks.

## Toolchain Images

The core of this repository is the set of Docker images located in the `/images` directory.

### `calico/base`

This is the base image upon which other toolchain images are built. It includes a common set of GNU C/C++ libraries, licenses, and configurations. It comes in two flavors: one on UBI 9 (`Dockerfile`) and another on UBI 8 (`Dockerfile.ubi8`).

### `calico/binfmt`

This image provides the necessary tools to enable a Linux host to run binaries from different architectures. It contains QEMU static binaries and a tool to register them with the kernel's `binfmt_misc` service. This is crucial for cross-compilation and running multi-architecture containers.

### `calico/go-build`

This image provides a comprehensive environment for building Go-based Calico components. It includes:

- A specific version of Go.
- Build tools like `controller-gen`.
- Configurations for different Linux distributions (e.g., AlmaLinux).
- Google Cloud SDK.
- Support for cross-compilation to different architectures (`amd64`, `arm64`, `ppc64le`).
- The `versions.yaml` file defines the specific versions of the tools to be installed in the image.

### `calico/rust-build`

This image provides an environment for building Rust-based Calico components. It includes:

- A specific version of the Rust toolchain.
- The `versions.yaml` file defines the specific versions of the tools to be installed in the image.

## Building and Usage

The `Makefile` at the root of the repository provides convenient targets for building and managing the images.

- `make image`: Builds the toolchain images for the host architecture.
- `ARCH=<arch> make image`: Builds the images for a specific target architecture.
- `make register`: Registers `binfmt_misc` handlers on the host to enable running binaries built for other architectures using QEMU.

### Cross-compilation

The `calico/go-build` image is designed to support cross-compilation. For example, you can build a binary for `arm64` on an `amd64` host by setting the `GOARCH` environment variable within the container.

## CI/CD and Automation

### Overview

This repository uses a combination of Semaphore CI and GitHub Actions to automate its build, release, and maintenance workflows.

- **Semaphore CI**: The primary CI/CD engine, responsible for building, testing, and publishing the Docker toolchain images to a container registry. The configuration is defined in `.semaphore/semaphore.yml`.
- **GitHub Actions**: Used for repository automation and management tasks, such as creating new release branches and version tags in response to dependency updates.

### Image Build and Publish Pipeline

The CI/CD pipeline is configured to automatically build and publish Docker images to Docker Hub whenever changes are merged into key branches.

- **On the `master` branch**:
  - A change in `images/calico-base/` triggers an update to the `calico/base` image.
  - A change in `images/calico-binfmt/` triggers an update to the `calico/binfmt` image.
  - A change in `images/calico-go-build/` triggers an update to the `calico/go-build` image.
  - A change in `images/calico-rust-build/` triggers an update to the `calico/rust-build` image.
- **On Release Branches (`go1.xy`)**: Any change merged into a release branch will also trigger a build and update of the corresponding images on Docker Hub, tagged appropriately for that release.

### Branching and Versioning Strategy

The repository follows a specific branching and versioning workflow to manage toolchain updates.

- **`master` Branch**: This is the primary development branch where all dependency updates (e.g., Go, LLVM, Rust) are first integrated via pull requests.
- **Release Branches (`go1.xy`)**: These branches (e.g., `go1.25`) correspond to a specific minor version of Go and represent a stable set of toolchains used for building official Calico releases.

The workflow for updating dependencies is as follows:

1. **Latest Version**: Updates for the newest toolchain versions are always merged into the `master` branch first.
2. **Cherry-Picking**: After merging to `master`, these changes are then cherry-picked into the latest release branch (e.g., a Go 1.25.6 update is merged to `master`, then picked to `go1.25`).
3. **Previous Version Maintenance**: The repository also maintains the previous stable release (e.g., `go1.24`). Updates for this older branch are merged directly into the release branch itself, bypassing `master`.

### Automated Branch and Tag Creation

To streamline the release process, GitHub Actions are used to automate the creation of new branches and tags when toolchain versions change.

- **New Go Version on `master`**: When a pull request modifying `images/calico-go-build/versions.yaml` is merged into `master`, a workflow is triggered to automatically:
  1. Generate and create a new Go-specific release branch (e.g., `go1.26`).
  2. Generate a new version tag based on the component versions (e.g., `1.25.6-llvm18.1.8-k8s1.30.3`).
  3. Push the new branch and tag to the repository.

- **Updates on Release Branches**: When a pull request is merged into an existing `go1.xy` release branch, a separate workflow is triggered to:
  1. Generate a new version tag.
  2. If the tag already exists, append a counter to create a unique tag (e.g., `1.25.6-llvm18.1.8-k8s1.30.3-1`).
  3. Push the new tag to the repository.

## Project Conventions

To maintain code quality, readability, and consistency across the repository, it is essential to adhere to standard and commonly accepted formatting conventions for all file types. Specifically:

- **Markdown (`.md`) files**: Follow standard Markdown syntax and formatting guidelines (e.g., consistent heading levels, list spacing, code block usage).
- **YAML (`.yaml`) files**: Adhere to YAML specification best practices, including consistent indentation (typically two spaces), proper key-value spacing, and logical structuring.
- **Go (`.go`) files**: Follow the official Go formatting conventions enforced by `gofmt` (e.g., proper indentation, brace placement, and declaration style). When making changes, ensure `gofmt` is run to automatically correct any formatting issues.
