// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"encoding/json"
	"fmt"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	consulv1aplha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Argo Rollouts addresses header routes by name: a setHeaderRoute step creates or updates
// "the route called X", and RemoveManagedRoutes deletes the routes the plugin owns. Consul's
// ServiceRoute has no name, no meta and no other inert field that could carry one -- it is
// exactly {Match, Destination}, and every field of both changes how traffic is routed. The
// name therefore has to live outside the route, on the ServiceRouter, and be tied back to a
// route by position.
//
// The scheme is:
//
//   - managedRoutesAnnotation holds a JSON array of route names that is positionally parallel
//     to a *prefix* of spec.routes: names[i] is the name of spec.routes[i].
//   - Managed routes always occupy the front of spec.routes. Consul is first-match-wins, so the
//     targeted audience has to outrank any route the user wrote themselves.
//   - Every read re-derives and checks the invariant instead of indexing blindly (see
//     managedRouteNames), so a desync is reported rather than silently stripping a route that
//     belongs to somebody else.
//
// Position, rather than a content hash of the route, is deliberate. consul-k8s runs a mutating
// admission webhook over every ServiceRouter write: ServiceRouter.DefaultNamespaceFields
// rewrites spec.routes[i].Destination.Namespace server-side when Consul namespaces are enabled.
// A hash computed by the plugin would therefore stop matching what is actually persisted and
// would orphan the plugin's own routes. That defaulting is in-place and index-preserving, and
// nothing in consul-k8s reorders spec.routes, so an index survives it where a hash does not.
const (
	// managedRoutesAnnotation holds a JSON array of the Argo Rollouts route names this plugin
	// manages, parallel to the first len(names) entries of spec.routes.
	managedRoutesAnnotation = "consul.rollouts.argoproj.io/managed-routes"

	// managedByAnnotation records which Rollout owns the managed routes on a ServiceRouter, as
	// "<namespace>/<name>". It exists so that a second writer -- another Rollout pointed at the
	// same Consul service -- is detected and reported instead of the two silently fighting over
	// the same prefix of spec.routes.
	managedByAnnotation = "consul.rollouts.argoproj.io/managed-by"

	// routerCreatedByPluginAnnotation marks a ServiceRouter that this plugin created from
	// nothing. Only such a router may be deleted during cleanup; one the user wrote themselves
	// is emptied of managed routes and left in place.
	routerCreatedByPluginAnnotation = "consul.rollouts.argoproj.io/created-by-plugin"
)

// rolloutRef identifies a Rollout for the managed-by marker.
func rolloutRef(rollout *v1alpha1.Rollout) string {
	return fmt.Sprintf("%s/%s", rollout.GetNamespace(), rollout.GetName())
}

// managedRouteNames returns the ordered names of the routes this plugin manages on router.
// By construction they are the first len(names) entries of router.Spec.Routes.
//
// The annotation and spec.routes are two pieces of state that have to agree, so rather than
// trusting the annotation this re-establishes the invariant on every read and fails loudly if
// it does not hold. Blind indexing on a desynced annotation would delete whichever routes
// happened to sit at those indexes, which may be the user's own.
//
// Three things are checked:
//
//  1. the name list is no longer than spec.routes, so the prefix exists at all;
//  2. names are unique, so that "the route called X" is unambiguous;
//  3. every route in the managed prefix actually targets canarySubsetName, which is the only
//     destination this plugin ever writes. This is the check that catches a desync caused by
//     somebody editing spec.routes without touching the annotation.
//
// Destination.Namespace and Destination.Partition are deliberately not compared: the consul-k8s
// mutating webhook defaults them server-side.
func managedRouteNames(router *consulv1aplha1.ServiceRouter, canarySubsetName string) ([]string, error) {
	raw := router.GetAnnotations()[managedRoutesAnnotation]
	if raw == "" {
		return nil, nil
	}

	var names []string
	if err := json.Unmarshal([]byte(raw), &names); err != nil {
		return nil, fmt.Errorf("consul service router %q has a malformed %s annotation (%q): %w",
			router.GetName(), managedRoutesAnnotation, raw, err)
	}

	if len(names) > len(router.Spec.Routes) {
		return nil, fmt.Errorf(
			"consul service router %q claims %d plugin-managed routes in its %s annotation but spec.routes only has %d entries; "+
				"refusing to guess which routes are managed",
			router.GetName(), len(names), managedRoutesAnnotation, len(router.Spec.Routes))
	}

	seen := make(map[string]struct{}, len(names))
	for i, name := range names {
		if name == "" {
			return nil, fmt.Errorf("consul service router %q has an empty route name at index %d of its %s annotation",
				router.GetName(), i, managedRoutesAnnotation)
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("consul service router %q lists the managed route %q more than once in its %s annotation",
				router.GetName(), name, managedRoutesAnnotation)
		}
		seen[name] = struct{}{}

		route := router.Spec.Routes[i]
		if route.Destination == nil || route.Destination.ServiceSubset != canarySubsetName {
			return nil, fmt.Errorf(
				"consul service router %q is out of sync with its %s annotation: spec.routes[%d] is recorded as the managed route %q "+
					"but does not target the canary subset %q; refusing to modify routes that may not be managed by this plugin",
				router.GetName(), managedRoutesAnnotation, i, name, canarySubsetName)
		}
	}

	return names, nil
}

// indexOfRoute returns the position of name in names, or -1.
func indexOfRoute(names []string, name string) int {
	for i, n := range names {
		if n == name {
			return i
		}
	}
	return -1
}

// validateRouterOwnership reports an error if a different Rollout already manages routes on
// this ServiceRouter. Two Rollouts sharing one Consul service would each assume the managed
// prefix is theirs and would trample each other's routes; SetWeight makes the same exclusivity
// assumption about the ServiceSplitter and ServiceResolver, it just never says so.
func validateRouterOwnership(router *consulv1aplha1.ServiceRouter, rollout *v1alpha1.Rollout) error {
	owner := router.GetAnnotations()[managedByAnnotation]
	if owner == "" || owner == rolloutRef(rollout) {
		return nil
	}
	return fmt.Errorf(
		"consul service router %q already has header routes managed by rollout %q (see the %s annotation); "+
			"rollout %q will not write to it. Two rollouts cannot manage the same consul service",
		router.GetName(), owner, managedByAnnotation, rolloutRef(rollout))
}

// routerCreatedByPlugin reports whether this plugin created the ServiceRouter, and may
// therefore delete it once nothing is left on it.
func routerCreatedByPlugin(router *consulv1aplha1.ServiceRouter) bool {
	return router.GetAnnotations()[routerCreatedByPluginAnnotation] == "true"
}

// setManagedRouteAnnotations records the managed route names and owning Rollout on router.
// When no managed routes remain both markers are dropped, which is what lets
// RemoveManagedRoutes return without writing on every subsequent reconcile.
func setManagedRouteAnnotations(router *consulv1aplha1.ServiceRouter, names []string, rollout *v1alpha1.Rollout) error {
	annotations := router.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}

	if len(names) == 0 {
		delete(annotations, managedRoutesAnnotation)
		delete(annotations, managedByAnnotation)
	} else {
		encoded, err := json.Marshal(names)
		if err != nil {
			return fmt.Errorf("failed to encode managed route names %v: %w", names, err)
		}
		annotations[managedRoutesAnnotation] = string(encoded)
		annotations[managedByAnnotation] = rolloutRef(rollout)
	}

	router.SetAnnotations(annotations)
	return nil
}

// buildCanaryHeaderRoute turns the match entries of a setHeaderRoute step into a SINGLE Consul
// route carrying every header matcher, destined for the canary subset.
//
// One route, not one per entry: Consul requires all header matchers within a route to match, so
// collapsing the entries into one route is what gives the AND documented on SetHeaderRoute.
//
// Naming the subset in the destination is load-bearing. Consul compiles a subset-named
// destination with getResolverNode() rather than getSplitterOrResolverNode(), so the route skips
// the ServiceSplitter that SetWeight manages and the matched audience reaches the canary at
// 100%, while Consul's implicit catch-all still sends everyone else through the splitter.
func buildCanaryHeaderRoute(matches []v1alpha1.HeaderRoutingMatch, serviceName, canarySubsetName string) (consulv1aplha1.ServiceRoute, error) {
	headers := make([]consulv1aplha1.ServiceRouteHTTPMatchHeader, 0, len(matches))
	for i, match := range matches {
		header, err := buildHeaderMatch(match)
		if err != nil {
			return consulv1aplha1.ServiceRoute{}, fmt.Errorf("match[%d]: %w", i, err)
		}
		headers = append(headers, header)
	}

	return consulv1aplha1.ServiceRoute{
		Match: &consulv1aplha1.ServiceRouteMatch{
			HTTP: &consulv1aplha1.ServiceRouteHTTPMatch{
				Header: headers,
			},
		},
		Destination: &consulv1aplha1.ServiceRouteDestination{
			Service:       serviceName,
			ServiceSubset: canarySubsetName,
		},
	}, nil
}

// buildHeaderMatch maps one Argo HeaderRoutingMatch onto one Consul header matcher.
//
// Argo's HeaderValue is a StringMatch of {exact, prefix, regex} -- there is no suffix, present or
// invert reachable through a rollout step, even though Consul supports all three. consul-k8s
// rejects a header match with more than one of exact/prefix/suffix/regex/present set, so exactly
// one is required here and a step that sets none or several is rejected before any write.
func buildHeaderMatch(match v1alpha1.HeaderRoutingMatch) (consulv1aplha1.ServiceRouteHTTPMatchHeader, error) {
	if match.HeaderName == "" {
		return consulv1aplha1.ServiceRouteHTTPMatchHeader{}, fmt.Errorf("headerName must be set")
	}
	if match.HeaderValue == nil {
		return consulv1aplha1.ServiceRouteHTTPMatchHeader{}, fmt.Errorf("headerValue must be set for header %q", match.HeaderName)
	}

	header := consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: match.HeaderName}
	set := 0
	if v := match.HeaderValue.Exact; v != "" {
		header.Exact = v
		set++
	}
	if v := match.HeaderValue.Prefix; v != "" {
		header.Prefix = v
		set++
	}
	if v := match.HeaderValue.Regex; v != "" {
		header.Regex = v
		set++
	}

	switch {
	case set == 0:
		return consulv1aplha1.ServiceRouteHTTPMatchHeader{},
			fmt.Errorf("headerValue for header %q must set exactly one of exact, prefix or regex", match.HeaderName)
	case set > 1:
		return consulv1aplha1.ServiceRouteHTTPMatchHeader{},
			fmt.Errorf("headerValue for header %q sets more than one of exact, prefix or regex; consul accepts at most one per header match", match.HeaderName)
	}

	return header, nil
}

// insertRouteAt returns routes with route inserted at index at, without aliasing the input.
func insertRouteAt(routes []consulv1aplha1.ServiceRoute, at int, route consulv1aplha1.ServiceRoute) []consulv1aplha1.ServiceRoute {
	out := make([]consulv1aplha1.ServiceRoute, 0, len(routes)+1)
	out = append(out, routes[:at]...)
	out = append(out, route)
	out = append(out, routes[at:]...)
	return out
}

// removeRouteAt returns routes with index at removed, without aliasing the input.
func removeRouteAt(routes []consulv1aplha1.ServiceRoute, at int) []consulv1aplha1.ServiceRoute {
	out := make([]consulv1aplha1.ServiceRoute, 0, len(routes)-1)
	out = append(out, routes[:at]...)
	out = append(out, routes[at+1:]...)
	return out
}

// routerWriteError annotates a rejected ServiceRouter write with the preconditions this plugin
// cannot create for itself. A header route only compiles if the service's discovery chain speaks
// http and the canary subset already exists on the ServiceResolver; neither is the plugin's to
// set up, and the raw admission error alone rarely makes that obvious.
func routerWriteError(err error, serviceName, canarySubsetName string) error {
	if apierrors.IsInvalid(err) || apierrors.IsBadRequest(err) {
		return fmt.Errorf("consul rejected the service router %q: %w. A header route requires the discovery chain for %q to use "+
			"protocol http (set it on a ServiceDefaults or ProxyDefaults) and the subset %q to exist on the service resolver; "+
			"the plugin creates neither",
			serviceName, err, serviceName, canarySubsetName)
	}
	return fmt.Errorf("failed to write consul service router %q: %w", serviceName, err)
}
