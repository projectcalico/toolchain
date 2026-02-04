---
name: update-go-build
description: "Updates the versions of Go and Kubernetes in the calico/go-build image."
---

This skill automates the process of updating the Go and Kubernetes versions for the `calico/go-build` image. The Kubernetes update is conservative and will only update to the latest patch release within the current minor version.

### Procedure

**Step 1: Read Current Configuration**

First, read the `versions.yaml` file to get the current versions of Go and Kubernetes. This is crucial for determining what to search for.

```python
print(default_api.read_file(file_path="images/calico-go-build/versions.yaml"))
```

**Step 2: Fetch Latest Go Version and Checksums**

Execute a web fetch to get the official Go downloads page. From the HTML, identify the latest stable version and the SHA256 checksums for the `amd64`, `arm64`, `ppc64le`, and `s390x` Linux tarballs.

```python
print(default_api.web_fetch(prompt="Fetch the content of https://go.dev/dl/ and find the latest stable Go version number and the SHA256 checksums for the Linux tarballs for amd64, arm64, ppc64le, and s390x architectures."))
```

**Step 3: Fetch Latest Kubernetes Patch Release**

Based on the current Kubernetes version read in Step 1, execute a web fetch to find the latest patch release for that *specific minor version*. **You must replace `<major.minor>` with the version from the file (e.g., '1.34').**

```python
print(default_api.web_fetch(prompt="Fetch the content of https://kubernetes.io/releases/ and find the latest patch release for the Kubernetes <major.minor> series."))
```

**Step 4: Compare Versions and Prepare Updates**

- Compare the latest Go version (Step 2) with the current version (Step 1).
- Compare the latest Kubernetes patch release (Step 3) with the current version (Step 1).
- If everything is up-to-date, stop and inform the user. Otherwise, plan the necessary `replace` operations.

**Step 5: Update `versions.yaml` File**

If any updates are needed, perform a single `replace` operation to update the entire `versions.yaml` file. This is more efficient than running multiple separate replacements.

**Critical**: You must construct the `<new_versions_yaml_content>` block with all the updated Go and Kubernetes information, while preserving the LLVM version exactly as it was.

```python
print(default_api.replace(
  file_path="images/calico-go-build/versions.yaml",
  instruction="Update Go and Kubernetes versions in a single operation.",
  old_string="""<original_versions_yaml_content>""",
  new_string="""<new_versions_yaml_content>"""
))
```

**Step 6: Verify and Propose Commit**

After updating the file, show the changes and propose a commit message.

```python
print(default_api.run_shell_command(command="git status && git diff HEAD"))
```

Propose a commit message, such as: `feat(go-build): Update Go to <new_version> and Kubernetes to <new_version>`.

**Step 7: Check for Release Branch**

Check if a release branch for this Go version already exists (e.g., `go1.26`).

```python
print(default_api.run_shell_command(command="git ls-remote --heads origin go1.XX")) # Replace 1.XX with the new Go minor version
```

- If it exists and you are on `master`, remind the user that this change might need to be cherry-picked.
- If it does not exist, remind the user that merging this to `master` will trigger the creation of the new branch.