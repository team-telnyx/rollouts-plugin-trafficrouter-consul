# rollouts-plugin-trafficrouter-consul
The Argo Rollouts plugin implementing traffic routing using Consul on Kubernetes to manage advanced subset-based routing for Canary deployments.

Consul's support for Argo Rollouts is currently limited to subset-based routing.

## Install Argo Rollouts Controller

There are three methods for installing the Argo Rollouts Controller with Consul on Kubernetes:

1. [Install Rollouts Using Helm and init containers](#install-rollouts-using-helm-and-binary). We recommend installing the Argo Rollouts Controllor using this method.
1. [Install Rollouts Using Helm and binary](#install-rollouts-using-helm-and-binary)
1. [Standalone installation](#stand-alone-installation)

After installing the controller, you must [apply the RBAC CRD](https://raw.githubusercontent.com/argoproj-labs/rollouts-plugin-trafficrouter-consul/main/yaml/rbac.yaml) to your Kubernetes cluster.

### Install Rollouts Using Helm and init containers

We recommend using this method to install this plugin.

Add the following code to your `values.yaml` file to configure the plugin:

```yaml
controller:
  initContainers:
    - name: copy-consul-plugin
      image: hashicorp/rollouts-plugin-trafficrouter-consul
      command: ["/bin/sh", "-c"]
      args:
        # Copy the binary from the image to the rollout container
        - cp /bin/rollouts-plugin-trafficrouter-consul /plugin-bin/hashicorp
      volumeMounts:
        - name: consul-plugin
          mountPath: /plugin-bin/hashicorp
  trafficRouterPlugins:
    - name: "hashicorp/consul"
      location: "file:///plugin-bin/hashicorp/rollouts-plugin-trafficrouter-consul"
  volumes:
    - name: consul-plugin
      emptyDir: {}
  volumeMounts:
    - name: consul-plugin
      mountPath: /plugin-bin/hashicorp
```

Then install the `argo-rollouts` and apply your updated values using Helm:

```shell-session
$ helm install argo-rollouts argo/argo-rollouts -f values.yaml -n argo-rollouts
```

### Install Rollouts Using Helm and binary

To build the binary and install Rollouts, complete the following steps:

1. Build this plugin using your preferred tool. For example, `make build`.
1. Mount the built plugin onto the `argo-rollouts` container.
1. Add the following code to your `values.yaml` file to configure the plugin:

```yaml
controller:
  trafficRouterPlugins:
    - name: "argoproj-labs/consul"
      location: "file:///plugin-bin/hashicorp/rollouts-plugin-trafficrouter-consul"
  volumes:
    - name: consul-route-plugin
      hostPath:
        # The path being mounted to, change this depending on your mount path
        path: /rollouts-plugin-trafficrouter-consul
        type: DirectoryOrCreate
  volumeMounts:
    - name: consul-route-plugin
      mountPath: /plugin-bin/hashicorp
```

Then install the `argo-rollouts` and apply your updated values using Helm:

```shell-session
  $ helm install argo-rollouts argo/argo-rollouts -f values.yaml -n argo-rollouts
```

### Stand-alone installation

This section describes the process to create a stand-alone installation. These instructions are for illustrative purposes. We recommend using init containers to create and install this plugin.

To create a stand-alone installation of the Rollouts plugin, complete the following steps:

1. Build this plugin.
1. Put the plugin on the path and mount it to the `argo-rollouts`container.
1. Create a `ConfigMap` to configure `argo-rollouts` with the plugin's location.

The following example schedules a Deployment and mounts it to the `argo-rollouts` container:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: argo-rollouts
  namespace: argo-rollouts
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: argo-rollouts
  template:
    spec:
      # ...
      volumes:
        # ...
         - name: consul-plugin
           hostPath:
             path: /plugin-bin/hashicorp/rollouts-plugin-trafficrouter-consul
             type: DirectoryOrCreate
      containers:
        - name: argo-rollouts
        # ...
          volumeMounts:
             - name: consul-route-plugin
               mountPath: /plugin-bin/hashicorp

```

The following example creates the `ConfigMap` with the location of the plugin:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: argo-rollouts-config
  namespace: argo-rollouts
data:
  trafficRouterPlugins: |
      [
        {
          "name": "argoproj-labs/consul",
          "location" : "file:///plugin-bin/hashicorp/rollouts-plugin-trafficrouter-consul"
        }
      ]      
binaryData: {}
```

### Install the RBAC

After either mounting the binary or using an init container, configure an RBAC using [Argo Rollout Consul plugin `rbac.yaml`](https://raw.githubusercontent.com/argoproj-labs/rollouts-plugin-trafficrouter-consul/main/yaml/rbac.yaml):

```shell-session
$ kubectl apply -f https://raw.githubusercontent.com/argoproj-labs/rollouts-plugin-trafficrouter-consul/main/yaml/rbac.yaml
```

## Use the Argo Rollouts Consul plugin

Schedule the Kubernetes Service utilized by the service being rolled out. Additionally, configure any service defaults and proxy defaults required for the service.

```yaml
apiVersion: v1
kind: Service
metadata:
  name: test-service
spec:
  selector:
    app: test-service
  ports:
    - name: http
      port: 80
      targetPort: 8080
```

Next, create the service resolver and service splitter CRDs for your stable service. Argo automatically modifies these CRDs during canary deployments.

The following example demonstrates the configuration of the service resolver CRD, which creates service subsets and sets the stable subset as the default:

```yaml
apiVersion: consul.hashicorp.com/v1alpha1
kind: ServiceResolver
metadata:
  name: test-service
spec:
  subsets:
    stable:
      filter: Service.Meta.version == 1
    canary:
      filter: ""
  defaultSubset: stable
```

The following example demonstrates the configuration of the service splitter CRD, which initially sends 100% of traffic to the stable deployment:

```yaml
apiVersion: consul.hashicorp.com/v1alpha1
kind: ServiceSplitter
metadata:
  name: test-service
spec:
  splits:
    - weight: 100
      serviceSubset: stable
    - weight: 0
      serviceSubset: canary
```

Then configure your Argo Rollout resource to incrementally rollout the canary deployment:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: test-service
spec:
  replicas: 3
  selector:
    matchLabels:
      app: test-service
  template:
    metadata:
      labels:
        app: test-service
      annotations:
        consul.hashicorp.com/connect-inject: "true"
        consul.hashicorp.com/service-meta-version: "1"
        consul.hashicorp.com/service-tags: "v1"
    spec:
      containers:
        - name: test-service
          # Using alpine vs latest as there is a build issue with M1s. Also other tests in multiport-app reference
          # alpine so standardizing this.
          image: docker.mirror.hashicorp.services/hashicorp/http-echo:alpine
          args:
            - -text="I am v1"
            - -listen=:8080
          ports:
            - containerPort: 8080
              name: http
      serviceAccountName: test-service
      terminationGracePeriodSeconds: 0 # so deletion is quick
  strategy:
    canary:
      trafficRouting:
        plugins:
          hashicorp/consul:
            stableSubsetName: stable # subset name of the stable service
            canarySubsetName: canary # subset name of the canary service
            serviceName: test-service
      steps:
      - setWeight: 20
      - pause: {}
      - setWeight: 40
      - pause: {duration: 10}
      - setWeight: 60
      - pause: {duration: 10}
      - setWeight: 80
      - pause: {duration: 10}
```

Finally, perform the Rollout operation using the Argo Rollouts Kubectl plugin.

```sh
kubectl argo rollouts promote test-service
```

## Header-based routing

A `setHeaderRoute` step pins a chosen audience to the canary while everyone else continues to be
split by weight. The plugin writes a Consul `ServiceRouter` named after `serviceName`, with a
route whose destination names `canarySubsetName`. Consul compiles a subset-named destination
with `getResolverNode()` rather than `getSplitterOrResolverNode()`, so that route bypasses the
`ServiceSplitter` and the matched audience reaches the canary at 100%, while Consul's implicit
catch-all still routes everybody else through the splitter that `setWeight` manages.

```yaml
      steps:
      - setHeaderRoute:
          name: canary-audience
          match:
          - headerName: user-agent
            headerValue:
              regex: ".*Firefox.*"
          - headerName: x-geo-region
            headerValue:
              exact: africa
      - pause: {}
      - setWeight: 20
      trafficRouting:
        managedRoutes:
        - name: canary-audience
```

### Match entries are ANDed, not ORed

**This plugin ANDs the `match` entries of a step; the built-in Istio reconciler ORs them.** The
step above produces a single Consul route carrying both header matchers, so only a request that
is *both* Firefox *and* from `africa` reaches the canary:

```yaml
spec:
  routes:
    - match:
        http:
          header:
            - {name: user-agent, regex: ".*Firefox.*"}
            - {name: x-geo-region, exact: africa}
      destination: {service: test-service, serviceSubset: canary}
```

Istio would emit one match per entry and route a request matching *either* header. The
difference is Consul's data model rather than a choice: all header matchers within a route must
match for the route to apply. Anyone reading the Argo Rollouts documentation should expect OR
and will get AND here.

OR is still reachable two ways: regex alternation within a single header
(`regex: ".*(Firefox|Safari).*"`), or several `setHeaderRoute` steps, each of which becomes its
own route.

Only `exact`, `prefix` and `regex` are reachable, because Argo Rollouts' `headerValue` offers
nothing else. Consul additionally supports `suffix`, `present` and `invert`, but no rollout step
can express them. Exactly one of the three must be set per entry: consul-k8s rejects a header
match with more than one of `exact`/`prefix`/`suffix`/`regex`/`present`.

### Preconditions the plugin cannot create

A header route only compiles if the service's discovery chain speaks HTTP, and if the canary
subset already exists on the `ServiceResolver`. The plugin creates neither, and surfaces the
admission error with a reminder when Consul rejects the write. Set the protocol on a
`ServiceDefaults` (or `ProxyDefaults`) before using `setHeaderRoute`:

```yaml
apiVersion: consul.hashicorp.com/v1alpha1
kind: ServiceDefaults
metadata:
  name: test-service
spec:
  protocol: http
```

### How named routes are tracked

Argo Rollouts addresses header routes by name, but a Consul `ServiceRoute` has only `match` and
`destination` — there is no name field, and no inert field that could carry one. The plugin
therefore records names on the `ServiceRouter` itself:

| Annotation | Meaning |
| --- | --- |
| `consul.rollouts.argoproj.io/managed-routes` | JSON array of route names, positionally parallel to a **prefix** of `spec.routes` |
| `consul.rollouts.argoproj.io/managed-by` | the `namespace/name` of the Rollout that owns those routes |
| `consul.rollouts.argoproj.io/created-by-plugin` | set when the plugin created the `ServiceRouter`, and may therefore delete it again |

Managed routes always occupy the **front** of `spec.routes`, because Consul is first-match-wins
and the targeted audience has to outrank any route you wrote yourself. Your own routes are
preserved, kept in order, and never modified.

The annotation is validated on every read rather than trusted: the name list may not be longer
than `spec.routes`, names must be unique, and every route in the managed prefix must actually
target the canary subset. If any of that fails the plugin reports an error instead of indexing
blindly into `spec.routes` and stripping a route that may be yours. Likewise, a `ServiceRouter`
whose `managed-by` names a different Rollout is refused: two Rollouts cannot drive one Consul
service.

On full promotion or abort the plugin removes the routes it owns and drops these annotations; it
deletes the `ServiceRouter` only if it created it and no routes remain.

# Testing
To run unit tests use `go test ./...`. For end-to-end verification follow the steps in `./testing/README.md`.