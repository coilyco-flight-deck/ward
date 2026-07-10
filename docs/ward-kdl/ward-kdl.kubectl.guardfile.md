# ward-kdl ops kubectl

Exec-dialect CLI. Every verb runs `/usr/local/bin/kubectl` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl ops kubectl get - list/show resources; pass type + name as args

`/usr/local/bin/kubectl get`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl describe - detailed resource state; pass type + name

`/usr/local/bin/kubectl describe`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl logs - pod/container logs; -f to follow, --previous for prior

`/usr/local/bin/kubectl logs`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl events - cluster events; --for to scope to a resource

`/usr/local/bin/kubectl events`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl top - resource usage; pass `pod` or `node`

`/usr/local/bin/kubectl top`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl explain - schema docs for a resource type

`/usr/local/bin/kubectl explain`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl api-resources - list served resource types

`/usr/local/bin/kubectl api-resources`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl api-versions - list served API group/versions

`/usr/local/bin/kubectl api-versions`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl cluster-info - control-plane + addon endpoints

`/usr/local/bin/kubectl cluster-info`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl version - client + server version

`/usr/local/bin/kubectl version`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl config current-context - name of the active kubeconfig context

`/usr/local/bin/kubectl config current-context`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl config get-contexts - list kubeconfig contexts (no secrets)

`/usr/local/bin/kubectl config get-contexts`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl diff - preview an apply: server-side dry-run diff of a manifest vs live state; -f <file> or -k <kustomize dir>

`/usr/local/bin/kubectl diff`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl apply - apply manifests; -f <file> or -k <kustomize dir>

`/usr/local/bin/kubectl apply`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl scale - set replica count on a workload

`/usr/local/bin/kubectl scale`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl rollout status - watch a rollout to completion

`/usr/local/bin/kubectl rollout status`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl rollout restart - trigger a rolling restart

`/usr/local/bin/kubectl rollout restart`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl rollout history - revision history for a workload

`/usr/local/bin/kubectl rollout history`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl rollout undo - roll a workload back to a prior revision

`/usr/local/bin/kubectl rollout undo`

Flags: unrestricted passthrough.

## See also

- [ward-kdl.md](../ward-kdl.md) - the build-time authoring layer behind this surface
- [ward-kdl-surface.md](../ward-kdl-surface.md) - the full generated verb surface, area by area
