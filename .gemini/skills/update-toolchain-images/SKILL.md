---
name: update-toolchain-images
description: "Updates toolchain versions for calico/go-build, calico/rust-build, or calico/binfmt images by dispatching to a specific sub-skill."
---

This is a master skill that acts as a dispatcher for updating a specific Calico toolchain image.

### Procedure

1. **Identify Target Image:** Determine which toolchain image the user wants to update (`calico/go-build`, `calico/rust-build`, or `calico/binfmt`). If the user's request is ambiguous (e.g., "update the toolchain"), ask for clarification.

2. **Activate the Specific Sub-Skill:** Based on the user's request, activate the corresponding sub-skill to perform the update.

    * **To update the `calico/rust-build` image:**
        * Inform the user you are activating the `update-rust-build` skill.
        * Execute the tool call: `activate_skill(name='update-rust-build')`
        * Follow the instructions provided by the activated skill.

    * **To update the `calico/go-build` image:**
        * Inform the user you are activating the `update-go-build` skill.
        * Execute the tool call: `activate_skill(name='update-go-build')`
        * Follow the instructions provided by the activated skill.

    * **To update the `calico/binfmt` image:**
        * Inform the user you are activating the `update-binfmt` skill.
        * Execute the tool call: `activate_skill(name='update-binfmt')`
        * Follow the instructions provided by the activated skill.
