---
doc_goal: Explain that Ward's native policy is embedded after operator config moved to AOSguard.
---
# Ward config source

Ward's native agent control plane uses baked AOS-authored role, fleet, topology,
and launch-policy assets. That keeps `ward agent`, `ward container`, `ward exec`,
help, and version independent of an operator configuration checkout.

`WARD_CONFIG_REF` is no longer a Ward runtime dependency. AOSguard owns operator
spec configuration inside the AOS image and exposes its generated APIs through
`aosguard ops ...`.

For compatibility examples that still need a Ward-owned value, use this repo's
own policy bundle from a checkout: `WARD_CONFIG_REF=file://$PWD/.ward`.

Ward retains typed Forgejo, GitHub, and Shortcut adapters only where its own
issue-to-merge workflow needs them. Those adapters do not route through AOSguard.
