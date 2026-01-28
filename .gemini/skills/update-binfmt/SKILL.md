---
name: update-binfmt
description: "Updates the version of QEMU in the calico/binfmt image."
---

This skill automates the process of updating the `qemu-user-static` version for the `calico/binfmt` image.

### Procedure

**Step 1: Read Current Configuration**

Read the `versions.yaml` file to get the current QEMU version.

```
print(default_api.read_file(file_path="images/calico-binfmt/versions.yaml"))
```

**Step 2: Fetch Latest Fedora Release**

Determine the latest stable Fedora release version. This is needed to find the correct QEMU package.

```
print(default_api.web_fetch(prompt="Find the latest stable release number for Fedora from https://www.fedoraproject.org/"))
```

**Step 3: Find Latest QEMU Package Version**

Using the latest Fedora release number from Step 2, find the latest `qemu-user-static` package version. Extract the base version (e.g., `9.0.0` from `9.0.0-1.fc40`). **You must replace `<fedora_version>` with the result from the previous step.**

```
print(default_api.web_fetch(prompt="Find the latest version of the qemu-user-static package for Fedora <fedora_version> from https://packages.fedoraproject.org/pkgs/qemu/qemu-user-static/"))
```

**Step 4: Extract and Compare QEMU Version**

From the package version string obtained in Step 3 (e.g., `9.0.0-1.fc40`), extract the base version (e.g., `9.0.0`). Compare this extracted base version with the `qemu.version` from `versions.yaml` (read in Step 1).

-   If the extracted QEMU base version is not newer than the current version in `versions.yaml`, stop. The skill is complete and no update is needed.
-   If it is newer, proceed to the next step.

**Step 5: Update `versions.yaml` with New QEMU Version**

If an update is needed, execute the following tool call. **You must replace `<old_qemu_version>` and `<new_qemu_version>` with the actual QEMU version numbers.**

```
print(default_api.replace(
  file_path="images/calico-binfmt/versions.yaml",
  instruction="Update the qemu version to the latest stable release.",
  old_string="  version: <old_qemu_version>",
  new_string="  version: <new_qemu_version>"
))
```

**Step 6: Verify and Propose Commit**

After updating the file, show the changes to the user and propose a conventional commit message.

```
print(default_api.run_shell_command(command="git status && git diff HEAD", description="Checking the git status and diff after modifying the QEMU version."))
```

Propose a commit message summarizing the changes, for example: `feat(binfmt): Update QEMU to version <new_qemu_version>`.
