# Gemini Code Understanding for Project Calico Toolchain

This document provides a comprehensive overview of the Project Calico toolchain repository. It is designed to assist the Gemini family of models in understanding the codebase, its purpose, and the development workflows.

## 1. Project Overview

This repository hosts the Docker-based toolchain images required to build [Project Calico](https://projectcalico.org) components. It provides consistent, versioned, and reproducible build environments (including Go, LLVM, and Rust) across multiple architectures.

## 2. File Structure

- **`.github/workflows/`**: GitHub Actions for release automation (branch/tag creation).
- **`.semaphore/`**: Semaphore CI configurations for building, testing, and publishing images.
- **`cmd/`**: Go source code for repository-specific tools.
  - `binfmt/`: Registers/unregisters `binfmt_misc` handlers.
  - `semvalidator/`: Validates Semaphore CI configurations.
- **`hack/`**: Shell scripts for CI/CD tasks (e.g., version tagging).
- **`images/`**: Source `Dockerfiles` and configurations for the toolchain images.
- **`Makefile*`**: Root Makefiles for building images and running local tasks.

## 3. Toolchain Images (`/images`)

The core artifacts of this repository are the Docker images defined in the `images/` directory:

| Image | Description | Key Contents |
| :--- | :--- | :--- |
| **`calico/base`** | Base image for other toolchains. | GNU C/C++ libraries, licenses, global configs. (UBI 8 & 9 variants). |
| **`calico/binfmt`** | Enables multi-arch builds on Linux. | QEMU static binaries, registration tool for `binfmt_misc`. |
| **`calico/go-build`** | Environment for building Go components. | Go toolchain, `controller-gen`, Google Cloud SDK, cross-compilation support (`amd64`, `arm64`, `ppc64le`, `s390x`). Versions defined in `versions.yaml`. |
| **`calico/rust-build`** | Environment for building Rust components. | Rust toolchain. Versions defined in `versions.yaml`. |

## 4. Development Workflow

### Building Images
Images are typically built on **`amd64`** runners and cross-compiled for other platforms. Use the root `Makefile` to build images locally:

- **Build for host architecture**:
  ```bash
  make image
  ```
- **Build for specific architecture**:
  ```bash
  ARCH=arm64 make image
  ```
- **Register QEMU (for multi-arch)**:
  ```bash
  make register
  ```

### Cross-Compilation
The `calico/go-build` image supports cross-compilation. Set the `GOARCH` environment variable inside the container to build for a different target architecture (e.g., building for `arm64` on an `amd64` host).

## 5. CI/CD & Release Process

### Automation Engines
- **Semaphore CI**: Builds, tests, and publishes images to Docker Hub. Configured in `.semaphore/semaphore.yml`.
- **GitHub Actions**: Manages repository ops (creating release branches, tagging versions).

### Triggering Builds
- **`master` Branch**: Merges to `master` trigger updates for the `latest` tag of the modified image(s).
- **Release Branches (`go1.xy`)**: Merges to release branches (e.g., `go1.25`) trigger updates for that specific stable line.

### Branching and Versioning Strategy
The repository follows a specific strategy for managing toolchain updates, centered around the Go version.

1.  **`master` Branch**: The primary development branch. All dependency updates (Go, LLVM, Rust) should be merged here first.
2.  **Release Branches (`go1.xy`)**: Stable branches corresponding to a Go minor version (e.g., `go1.25`). These branches are used to build official Calico releases.

#### Workflow for Updates
1.  **New Go Minor Version (e.g., 1.26.0)**:
    *   Update `versions.yaml` in `master`.
    *   **Automation**: Merging to `master` automatically creates a new branch `go1.26` and a corresponding initial tag.
2.  **Updates to Existing Release (e.g., Go 1.25.6 -> 1.25.7)**:
    *   **Step 1**: Update `versions.yaml` in `master` first. (This updates `latest` but *does not* trigger a new tag or branch if `go1.25` already exists).
    *   **Step 2**: Cherry-pick the change into the `go1.25` branch.
    *   **Automation**: Merging to `go1.25` triggers the creation of a new release tag (e.g., `1.25.7-llvm...`).
3.  **Old Releases**: Updates for older branches (e.g., `go1.24`) are made directly on that branch.

### Automated Branch and Tag Creation
GitHub Actions automate the creation of branches and tags based on file changes:

-   **On `master`**:
    *   Trigger: Pull Request merge.
    *   Logic: Checks `images/calico-go-build/versions.yaml`.
    *   Action: If the corresponding `go1.xy` branch **does not exist**, it creates the branch and pushes an initial tag. **If the branch exists, no action is taken.**

-   **On `go1.xy` branches**:
    *   Trigger: Pull Request merge.
    *   Logic: Checks `images/calico-go-build/versions.yaml`.
    *   Action: Generates a tag (e.g., `1.25.6-llvm18.1.8-k8s1.34.3`). If the tag exists, it appends a counter (e.g., `-1`) to ensure uniqueness.

## 6. Project Conventions

To ensure consistency and code quality:

- **Markdown**: Use standard Markdown. Maintain hierarchy and readability.
- **YAML**: Use 2-space indentation. Keep structures logical and consistent.
- **Go**: **ALWAYS** run `gofmt` on `.go` files before committing.
- **Commit Messages**: Follow standard conventional commits (e.g., `feat(go-build): update Go to 1.21.5`).
- **Verification**: Before finishing a task, verify changes by running relevant build commands (e.g., `make image`) or checking `git diff`.