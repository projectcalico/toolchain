---
name: update-rust-build
description: "Updates the version of Rust in the calico/rust-build image to the latest stable release."
---

This skill automates the process of updating the toolchain version for the `calico/rust-build` image to the latest stable version available.

### Procedure

**Step 1: Fetch Latest Stable Rust Version**

Execute the following tool call to get the content of the official Rust release page. From the output, identify the latest stable version number (e.g., "Stable: X.Y.Z").

```
print(default_api.web_fetch(prompt="Fetch the content of https://releases.rs/ and find the latest stable Rust version number."))
```

**Step 2: Read Current Rust Version**

Execute the following tool call to read the `versions.yaml` file and determine the currently configured Rust version.

```
print(default_api.read_file(file_path="images/calico-rust-build/versions.yaml"))
```

**Step 3: Compare Versions**

Compare the latest stable version from Step 1 with the current version from Step 2.

-   If the versions are the same, the toolchain is already up-to-date. Stop the process and inform the user.
-   If the website version is newer, proceed to the next step.

**Step 4: Update Configuration File**

If an update is needed, execute the following tool call. **You must replace `<old_version>` and `<new_version>` with the actual version numbers from the previous steps.**

```
print(default_api.replace(file_path="images/calico-rust-build/versions.yaml",
  instruction:"Update the Rust version to the latest stable release.",
  old_string:"  version: <old_version>",
  new_string:"  version: <new_version>"))
```

**Step 5: Verify and Propose Commit**

After updating the file, show the changes to the user and propose a conventional commit message.

```
print(default_api.run_shell_command(command="git status && git diff HEAD", description="Checking the git status and diff after modifying the Rust version."))
```

Then, inform the user of the change and propose a commit message like: "feat(rust-build): Update Rust to version <new_version>".