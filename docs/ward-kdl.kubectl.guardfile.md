# ward-kdl ops kubectl

Exec-dialect CLI. Every verb runs `kubectl` with the granted subcommand (or its `argv` override) appended; the binary and its prefix are fixed and the caller can never substitute them.

## ward-kdl ops kubectl get - list/show resources; pass type + name as args

`kubectl get`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl describe - detailed resource state; pass type + name

`kubectl describe`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl logs - pod/container logs; -f to follow, --previous for prior

`kubectl logs`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl events - cluster events; --for to scope to a resource

`kubectl events`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl top - resource usage; pass `pod` or `node`

`kubectl top`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl explain - schema docs for a resource type

`kubectl explain`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl api-resources - list served resource types

`kubectl api-resources`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl api-versions - list served API group/versions

`kubectl api-versions`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl cluster-info - control-plane + addon endpoints

`kubectl cluster-info`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl version - client + server version

`kubectl version`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl config current-context - name of the active kubeconfig context

`kubectl config current-context`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl config get-contexts - list kubeconfig contexts (no secrets)

`kubectl config get-contexts`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl apply - apply manifests; -f <file> or -k <kustomize dir>

`kubectl apply`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl scale - set replica count on a workload

`kubectl scale`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl rollout status - watch a rollout to completion

`kubectl rollout status`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl rollout restart - trigger a rolling restart

`kubectl rollout restart`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl rollout history - revision history for a workload

`kubectl rollout history`

Flags: unrestricted passthrough.

## ward-kdl ops kubectl rollout undo - roll a workload back to a prior revision

`kubectl rollout undo`

Flags: unrestricted passthrough.
