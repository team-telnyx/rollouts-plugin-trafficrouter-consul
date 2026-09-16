// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	rolloutsPlugin "github.com/argoproj/argo-rollouts/rollout/trafficrouting/plugin/rpc"
	pluginTypes "github.com/argoproj/argo-rollouts/utils/plugin/types"
	consulv1aplha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/argoproj-labs/rollouts-plugin-trafficrouter-consul/pkg/utils"
)

const (
	serviceMetaVersionAnnotation     = "consul.hashicorp.com/service-meta-%s"
	filterServiceMetaVersionTemplate = "Service.Meta.%s == %q"
)

// ConsulTrafficRouting represents the parameters required to configure the Consul Traffic Routing plugin
type ConsulTrafficRouting struct {
	ServiceName                 string `json:"serviceName" protobuf:"bytes,1,opt,name=serviceName"`
	CanarySubsetName            string `json:"canarySubsetName" protobuf:"bytes,2,opt,name=canarySubsetName"`
	StableSubsetName            string `json:"stableSubsetName" protobuf:"bytes,3,opt,name=stableSubsetName"`
	ServiceMetaAnnotationSuffix string `json:"serviceMetaAnnotationSuffix" protobuf:"bytes,4,opt,name=serviceMetaAnnotationSuffix"`
}

// RpcPlugin is the implementation of the TrafficRouterPlugin interface
type RpcPlugin struct {
	K8SClient client.Client
	LogCtx    *logrus.Entry
	IsTest    bool
}

var _ rolloutsPlugin.TrafficRouterPlugin = (*RpcPlugin)(nil)

// InitPlugin initializes the plugin adding the consul scheme to the k8s client
func (r *RpcPlugin) InitPlugin() pluginTypes.RpcError {
	if r.IsTest {
		return pluginTypes.RpcError{}
	}

	cfg, err := utils.NewKubeConfig()
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	s := runtime.NewScheme()
	if err := consulv1aplha1.AddToScheme(s); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	r.K8SClient, err = client.New(cfg, client.Options{Scheme: s})
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	return pluginTypes.RpcError{}
}

// SetWeight is called each time the rollout is updated to set the weight of the subsets
func (r *RpcPlugin) SetWeight(rollout *v1alpha1.Rollout, desiredWeight int32, _ []v1alpha1.WeightDestination) pluginTypes.RpcError {
	ctx := context.TODO()
	consulConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	serviceName := consulConfig.ServiceName
	canarySubsetName := consulConfig.CanarySubsetName
	stableSubsetName := consulConfig.StableSubsetName
	var suffix string
	if consulConfig.ServiceMetaAnnotationSuffix != "" {
		suffix = consulConfig.ServiceMetaAnnotationSuffix
	} else {
		suffix = "version"
	}
	serviceMetaVersion := rollout.Spec.Template.GetObjectMeta().GetAnnotations()[fmt.Sprintf(serviceMetaVersionAnnotation, suffix)]

	// This checks that we are performing a canary rollout, it is not
	// an error if this is empty. This will be empty on the initial rollout
	if rollout.Status.Canary == (v1alpha1.CanaryStatus{}) {
		r.LogCtx.WithFields(logrus.Fields{"desiredWeight": desiredWeight}).Debug("Rollout does not have a CanaryStatus yet")
		return pluginTypes.RpcError{}
	}

	// Get the service resolver
	serviceResolver := &consulv1aplha1.ServiceResolver{}
	if err := r.K8SClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: rollout.GetNamespace()}, serviceResolver, &client.GetOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	if err := validateResolverSyncStatus(serviceResolver); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// If the rollout is successful (not aborted) then modify the resolver
	if rolloutAborted(rollout) {
		r.LogCtx.WithFields(logrus.Fields{"canarySubsetName": canarySubsetName, "serviceResolver": serviceResolver}).Debug("Updating ServiceResolver for aborted rollout")
		serviceResolver, err = r.updateResolverForAbortedRollout(canarySubsetName, serviceResolver)
		if err != nil {
			return pluginTypes.RpcError{ErrorString: err.Error()}
		}
	} else {
		// Check if the pods have completely rolled over, and we are finished, now set the resolver to the stable version
		if rolloutComplete(rollout) {
			r.LogCtx.WithFields(logrus.Fields{"stableSubsetName": stableSubsetName, "canarySubsetName": canarySubsetName, "serviceMetaVersion": serviceMetaVersion, "serviceResolver": serviceResolver}).Debug("Updating ServiceResolver after completion")
			serviceResolver, err = r.updateResolverAfterCompletion(stableSubsetName, canarySubsetName, serviceMetaVersion, suffix, serviceResolver)
			if err != nil {
				return pluginTypes.RpcError{ErrorString: err.Error()}
			}
		} else {
			// Update the resolver so that canary subset points to the desired version
			r.LogCtx.WithFields(logrus.Fields{"canarySubsetName": canarySubsetName, "serviceMetaVersion": serviceMetaVersion, "serviceResolver": serviceResolver}).Debug("Updating ServiceResolver for in progress rollout")
			serviceResolver, err = r.updateResolverForInProgressRollouts(canarySubsetName, serviceMetaVersion, suffix, serviceResolver)
			if err != nil {
				return pluginTypes.RpcError{ErrorString: err.Error()}
			}
		}
	}

	// Get the service splitter
	serviceSplitter := &consulv1aplha1.ServiceSplitter{}
	if err := r.K8SClient.Get(ctx, types.NamespacedName{Name: serviceName, Namespace: rollout.GetNamespace()}, serviceSplitter, &client.GetOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	if err := validateSplitterSyncStatus(serviceSplitter); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// Assure tha the split exists
	if len(serviceSplitter.Spec.Splits) == 0 {
		return pluginTypes.RpcError{ErrorString: "spec.splits was not found in consul service splitter"}
	}

	// Assure that the split only contains the supported two subsets
	if len(serviceSplitter.Spec.Splits) != 2 {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("unexpected number of service splits. Expected 2, found %d", len(serviceSplitter.Spec.Splits))}
	}

	// We only expect there to be two splits, one for the canary and one for the stable
	// The canary subset should be the first split, represented by the desiredWeight (a percentage value), and the
	// stable subset should be the second split, represented by 100% - desiredWeight
	for i, split := range serviceSplitter.Spec.Splits {
		switch split.ServiceSubset {
		case canarySubsetName:
			serviceSplitter.Spec.Splits[i].Weight = float32(desiredWeight)
		case stableSubsetName:
			serviceSplitter.Spec.Splits[i].Weight = float32(100 - desiredWeight)
		default:
			return pluginTypes.RpcError{ErrorString: "unexpected service split"}
		}
	}

	// Persist resources at end of function to prevent writing to the cluster if there is an error
	// Persist changes to the ServiceSplitter
	r.LogCtx.WithFields(logrus.Fields{"serviceSplitter": serviceSplitter}).Debug("Updating ServiceSplitter")
	if err := r.K8SClient.Update(ctx, serviceSplitter, &client.UpdateOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// Persist changes to the ServiceResolver
	r.LogCtx.WithFields(logrus.Fields{"serviceResolver": serviceResolver}).Debug("Updating ServiceResolver")
	if err := r.K8SClient.Update(ctx, serviceResolver, &client.UpdateOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	return pluginTypes.RpcError{}
}

// Type returns the type of the plugin
func (r *RpcPlugin) Type() string {
	return Type
}

// UpdateHash is currently an empty stub to satisfy the interface
func (r *RpcPlugin) UpdateHash(_ *v1alpha1.Rollout, _, _ string, _ []v1alpha1.WeightDestination) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

// SetHeaderRoute upserts a Consul ServiceRouter route that sends every request matching the
// step's headers to the canary subset, bypassing the weighted split.
//
// # Match entries are ANDed, which differs from the built-in reconcilers
//
// A setHeaderRoute step carries a list of match entries. This plugin collapses them into ONE
// Consul route carrying every header matcher, so a request must carry ALL of the headers to be
// routed to the canary. The built-in Istio reconciler does the opposite: it emits one Istio
// match per entry, and Istio ORs across them, so any one header is enough. Anyone reading the
// Argo Rollouts documentation will expect OR; on Consul it is AND.
//
// That is not a choice so much as Consul's data model: all header matchers within a route must
// match for the route to apply. OR is still reachable -- use regex alternation for a single
// header, or several setHeaderRoute steps, each of which becomes its own route.
//
// # Behaviour
//
// A populated Match upserts the route named by setHeaderRoute.Name. An empty Match removes that
// named route, which is how Argo Rollouts expresses removal. Routes are addressed by name
// through the annotation scheme described in managedroutes.go, since a Consul ServiceRoute has
// no name of its own.
//
// The ServiceRouter is created if it does not exist. Everything is built in memory and written
// once at the end, so a rejected step leaves the cluster untouched.
func (r *RpcPlugin) SetHeaderRoute(rollout *v1alpha1.Rollout, headerRoute *v1alpha1.SetHeaderRoute) pluginTypes.RpcError {
	ctx := context.TODO()
	consulConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	if headerRoute == nil || headerRoute.Name == "" {
		return pluginTypes.RpcError{ErrorString: "setHeaderRoute step is missing a route name"}
	}

	serviceName := consulConfig.ServiceName
	canarySubsetName := consulConfig.CanarySubsetName

	// This checks that we are performing a canary rollout, it is not an error if this is empty.
	// This will be empty on the initial rollout. Same guard as SetWeight.
	if rollout.Status.Canary == (v1alpha1.CanaryStatus{}) {
		r.LogCtx.WithFields(logrus.Fields{"headerRoute": headerRoute.Name}).Debug("Rollout does not have a CanaryStatus yet")
		return pluginTypes.RpcError{}
	}

	namespacedName := types.NamespacedName{Name: serviceName, Namespace: rollout.GetNamespace()}
	serviceRouter := &consulv1aplha1.ServiceRouter{}
	routerExists := true
	if err := r.K8SClient.Get(ctx, namespacedName, serviceRouter, &client.GetOptions{}); err != nil {
		if !apierrors.IsNotFound(err) {
			return pluginTypes.RpcError{ErrorString: err.Error()}
		}
		routerExists = false
	}

	if !routerExists {
		// Removing a route from a router that does not exist is already satisfied. Return
		// before creating an empty router nobody asked for.
		if len(headerRoute.Match) == 0 {
			r.LogCtx.WithFields(logrus.Fields{"headerRoute": headerRoute.Name, "serviceRouter": serviceName}).
				Debug("No ServiceRouter to remove header route from")
			return pluginTypes.RpcError{}
		}
		serviceRouter = &consulv1aplha1.ServiceRouter{
			ObjectMeta: metav1.ObjectMeta{
				Name:      serviceName,
				Namespace: rollout.GetNamespace(),
				Annotations: map[string]string{
					routerCreatedByPluginAnnotation: "true",
				},
			},
		}
	} else if err := validateRouterSyncStatus(serviceRouter); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	if err := validateRouterOwnership(serviceRouter, rollout); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	managedNames, err := managedRouteNames(serviceRouter, canarySubsetName)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}
	index := indexOfRoute(managedNames, headerRoute.Name)

	// An empty Match is how Argo Rollouts asks for the named route to be removed.
	if len(headerRoute.Match) == 0 {
		if index < 0 {
			r.LogCtx.WithFields(logrus.Fields{"headerRoute": headerRoute.Name, "serviceRouter": serviceName}).
				Debug("Header route is not present on the ServiceRouter, nothing to remove")
			return pluginTypes.RpcError{}
		}

		serviceRouter.Spec.Routes = removeRouteAt(serviceRouter.Spec.Routes, index)
		managedNames = append(managedNames[:index:index], managedNames[index+1:]...)
		if err := setManagedRouteAnnotations(serviceRouter, managedNames, rollout); err != nil {
			return pluginTypes.RpcError{ErrorString: err.Error()}
		}

		r.LogCtx.WithFields(logrus.Fields{"headerRoute": headerRoute.Name, "serviceRouter": serviceRouter}).Debug("Removing header route from ServiceRouter")
		if err := r.persistRouterAfterRemoval(ctx, serviceRouter); err != nil {
			return pluginTypes.RpcError{ErrorString: routerWriteError(err, serviceName, canarySubsetName).Error()}
		}
		return pluginTypes.RpcError{}
	}

	route, err := buildCanaryHeaderRoute(headerRoute.Match, serviceName, canarySubsetName)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: fmt.Sprintf("invalid setHeaderRoute step %q: %s", headerRoute.Name, err.Error())}
	}

	if index >= 0 {
		// Argo re-reconciles while the step is current, so an upsert of a route already present
		// has to replace it in place and leave the route count alone.
		serviceRouter.Spec.Routes[index] = route
	} else {
		// New managed routes go at the end of the managed prefix: still ahead of anything the
		// user wrote, because Consul stops at the first matching route.
		serviceRouter.Spec.Routes = insertRouteAt(serviceRouter.Spec.Routes, len(managedNames), route)
		managedNames = append(managedNames, headerRoute.Name)
	}

	if err := setManagedRouteAnnotations(serviceRouter, managedNames, rollout); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// Persist resources at end of function to prevent writing to the cluster if there is an error
	r.LogCtx.WithFields(logrus.Fields{"headerRoute": headerRoute.Name, "serviceRouter": serviceRouter}).Debug("Updating ServiceRouter with header route")
	if routerExists {
		if err := r.K8SClient.Update(ctx, serviceRouter, &client.UpdateOptions{}); err != nil {
			return pluginTypes.RpcError{ErrorString: routerWriteError(err, serviceName, canarySubsetName).Error()}
		}
		return pluginTypes.RpcError{}
	}
	if err := r.K8SClient.Create(ctx, serviceRouter, &client.CreateOptions{}); err != nil {
		return pluginTypes.RpcError{ErrorString: routerWriteError(err, serviceName, canarySubsetName).Error()}
	}
	return pluginTypes.RpcError{}
}

// VerifyWeight is currently an empty stub to satisfy the interface
func (r *RpcPlugin) VerifyWeight(_ *v1alpha1.Rollout, _ int32, _ []v1alpha1.WeightDestination) (pluginTypes.RpcVerified, pluginTypes.RpcError) {
	return pluginTypes.NotImplemented, pluginTypes.RpcError{}
}

// SetMirrorRoute is currently an empty stub to satisfy the interface
func (r *RpcPlugin) SetMirrorRoute(_ *v1alpha1.Rollout, _ *v1alpha1.SetMirrorRoute) pluginTypes.RpcError {
	return pluginTypes.RpcError{}
}

// RemoveManagedRoutes drops every route this plugin owns from the Consul ServiceRouter, and
// deletes the ServiceRouter outright if the plugin created it and nothing is left on it. Routes
// the user wrote themselves are preserved.
//
// Argo Rollouts calls this on three separate paths -- full promotion, abort, and promote-full --
// and the promoted case fires on EVERY reconcile for the rest of the rollout's life. The early
// return when there is nothing of ours on the router is therefore load-bearing rather than an
// optimisation: without it every promoted service would issue a pointless write on every
// reconcile, against the API server and through a failurePolicy: Fail admission webhook.
// Dropping the annotations as part of removal is what makes that early return reachable on the
// reconcile after a successful cleanup.
func (r *RpcPlugin) RemoveManagedRoutes(rollout *v1alpha1.Rollout) pluginTypes.RpcError {
	ctx := context.TODO()
	consulConfig, err := getPluginConfig(rollout)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	serviceName := consulConfig.ServiceName
	canarySubsetName := consulConfig.CanarySubsetName

	namespacedName := types.NamespacedName{Name: serviceName, Namespace: rollout.GetNamespace()}
	serviceRouter := &consulv1aplha1.ServiceRouter{}
	if err := r.K8SClient.Get(ctx, namespacedName, serviceRouter, &client.GetOptions{}); err != nil {
		if apierrors.IsNotFound(err) {
			// No router at all: nothing to remove, and nothing to write.
			return pluginTypes.RpcError{}
		}
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// If another Rollout owns the routes on this router they are not ours to remove. Unlike
	// SetHeaderRoute this warns rather than erroring: RemoveManagedRoutes runs on every reconcile
	// of every promoted rollout, so a permanent error here would wedge a rollout that has nothing
	// to clean up. The misconfiguration still surfaces loudly on the write path.
	if owner := serviceRouter.GetAnnotations()[managedByAnnotation]; owner != "" && owner != rolloutRef(rollout) {
		r.LogCtx.WithFields(logrus.Fields{"serviceRouter": serviceName, "owner": owner, "rollout": rolloutRef(rollout)}).
			Warn("ServiceRouter header routes are managed by a different rollout, not removing them")
		return pluginTypes.RpcError{}
	}

	managedNames, err := managedRouteNames(serviceRouter, canarySubsetName)
	if err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// The load-bearing early return. This is the steady state for every fully promoted rollout
	// that either never used a setHeaderRoute step or has already been cleaned up.
	if len(managedNames) == 0 {
		return pluginTypes.RpcError{}
	}

	if err := validateRouterSyncStatus(serviceRouter); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	// managedRouteNames has already established that the managed routes are exactly the first
	// len(managedNames) entries, so everything after them is the user's and is kept.
	remaining := make([]consulv1aplha1.ServiceRoute, 0, len(serviceRouter.Spec.Routes)-len(managedNames))
	remaining = append(remaining, serviceRouter.Spec.Routes[len(managedNames):]...)
	serviceRouter.Spec.Routes = remaining
	if err := setManagedRouteAnnotations(serviceRouter, nil, rollout); err != nil {
		return pluginTypes.RpcError{ErrorString: err.Error()}
	}

	r.LogCtx.WithFields(logrus.Fields{"removedRoutes": managedNames, "serviceRouter": serviceRouter}).Debug("Removing managed routes from ServiceRouter")
	if err := r.persistRouterAfterRemoval(ctx, serviceRouter); err != nil {
		return pluginTypes.RpcError{ErrorString: routerWriteError(err, serviceName, canarySubsetName).Error()}
	}
	return pluginTypes.RpcError{}
}

// persistRouterAfterRemoval writes back a ServiceRouter that managed routes were just dropped
// from. A router the plugin created and that now holds nothing is deleted rather than left
// behind as an empty resource; a router the user created is always kept, even when empty.
func (r *RpcPlugin) persistRouterAfterRemoval(ctx context.Context, serviceRouter *consulv1aplha1.ServiceRouter) error {
	if len(serviceRouter.Spec.Routes) == 0 && routerCreatedByPlugin(serviceRouter) {
		return r.K8SClient.Delete(ctx, serviceRouter, &client.DeleteOptions{})
	}
	return r.K8SClient.Update(ctx, serviceRouter, &client.UpdateOptions{})
}

func (r *RpcPlugin) updateResolverAfterCompletion(stableSubsetName, canarySubsetName, serviceMetaVersion, suffix string, sr *consulv1aplha1.ServiceResolver) (*consulv1aplha1.ServiceResolver, error) {
	var err error
	sr, err = r.updateResolverSubsetForRollouts(canarySubsetName, "", sr)
	if err != nil {
		return nil, err
	}
	// Update the resolver so that stable subset points to the former canary version
	sr, err = r.updateResolverSubsetForRollouts(stableSubsetName, fmt.Sprintf(filterServiceMetaVersionTemplate, suffix, serviceMetaVersion), sr)
	if err != nil {
		return nil, err
	}
	return sr, nil
}

// updateResolverForInProgressRollouts sets the canary filter to the serviceMetaVersion passed in
func (r *RpcPlugin) updateResolverForInProgressRollouts(canarySubsetName, serviceMetaVersion, suffix string, sr *consulv1aplha1.ServiceResolver) (*consulv1aplha1.ServiceResolver, error) {
	return r.updateResolverSubsetForRollouts(canarySubsetName, fmt.Sprintf(filterServiceMetaVersionTemplate, suffix, serviceMetaVersion), sr)
}

// updateResolverForAbortedRollout sets the canary filter to empty if we've aborted the rollout
func (r *RpcPlugin) updateResolverForAbortedRollout(canarySubsetName string, sr *consulv1aplha1.ServiceResolver) (*consulv1aplha1.ServiceResolver, error) {
	return r.updateResolverSubsetForRollouts(canarySubsetName, "", sr)
}

func (r *RpcPlugin) updateResolverSubsetForRollouts(subsetName, filterValue string, sr *consulv1aplha1.ServiceResolver) (*consulv1aplha1.ServiceResolver, error) {
	if _, ok := sr.Spec.Subsets[subsetName]; !ok {
		return nil, fmt.Errorf("spec.subsets.%s.filter was not found in consul service resolver: %v", subsetName, sr)
	}
	subset := sr.Spec.Subsets[subsetName]
	subset.Filter = filterValue
	sr.Spec.Subsets[subsetName] = subset

	return sr, nil
}

func rolloutComplete(rollout *v1alpha1.Rollout) bool {
	rolloutCondition, err := completeCondition(rollout)
	if err != nil {
		return false
	}
	return strconv.FormatInt(rollout.GetObjectMeta().GetGeneration(), 10) == rollout.Status.ObservedGeneration &&
		rolloutCondition.Status == corev1.ConditionTrue
}

func completeCondition(rollout *v1alpha1.Rollout) (v1alpha1.RolloutCondition, error) {
	for i, condition := range rollout.Status.Conditions {
		if condition.Type == v1alpha1.RolloutCompleted {
			return rollout.Status.Conditions[i], nil
		}
	}
	return v1alpha1.RolloutCondition{}, errors.New("condition RolloutCompleted not found")
}

func rolloutAborted(rollout *v1alpha1.Rollout) bool {
	return rollout.Status.Abort
}

func getPluginConfig(rollout *v1alpha1.Rollout) (*ConsulTrafficRouting, error) {
	consulConfig := ConsulTrafficRouting{}
	if err := json.Unmarshal(rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[ConfigKey], &consulConfig); err != nil {
		return nil, err
	}
	if err := validateConfig(consulConfig); err != nil {
		return nil, err
	}
	return &consulConfig, nil
}

func validateConfig(cfg ConsulTrafficRouting) error {
	if cfg.StableSubsetName == "" || cfg.CanarySubsetName == "" || cfg.ServiceName == "" {
		return errors.New("invalid consul traffic routing configuration. stableSubsetName, canarySubsetName, and serviceName must be set")
	}
	return nil
}

// validateSyncStatus checks if a Consul config entry has synced, this is necessary to ensure that
// the resource is up-to-date before the rollout can continue. kind names the resource in the
// resulting error, e.g. "service resolver".
//
// Status.LastSyncedTime is a *metav1.Time and is nil until the consul-k8s controller has synced
// the resource at least once. A resource carrying a Synced condition but no lastSyncedTime is
// therefore possible, and dereferencing the pointer unconditionally panicked the plugin; a nil
// lastSyncedTime is treated as "not synced yet", which is what it means.
func validateSyncStatus(kind string, status consulv1aplha1.Status) error {
	for _, condition := range status.Conditions {
		if condition.Type == consulv1aplha1.ConditionSynced {
			if condition.Status != corev1.ConditionTrue ||
				status.LastSyncedTime == nil ||
				overTwoSeconds(condition.LastTransitionTime.Time, status.LastSyncedTime.Time) {
				return fmt.Errorf("%s has not synced with Consul. The %s needs to be up to date before rollout can continue", kind, kind)
			}
		}
	}
	return nil
}

// validateResolverSyncStatus checks if the resolver has synced with Consul, this is necessary to ensure that the resolver
// is up-to-date before the rollout can continue
func validateResolverSyncStatus(resolver *consulv1aplha1.ServiceResolver) error {
	return validateSyncStatus("service resolver", resolver.Status)
}

// validateSplitterSyncStatus checks if the splitter has synced with Consul, this is necessary to ensure that the splitter
// is up-to-date before the rollout can continue
func validateSplitterSyncStatus(splitter *consulv1aplha1.ServiceSplitter) error {
	return validateSyncStatus("service splitter", splitter.Status)
}

// validateRouterSyncStatus checks if the router has synced with Consul, this is necessary to ensure that the router
// is up-to-date before the rollout can continue
func validateRouterSyncStatus(router *consulv1aplha1.ServiceRouter) error {
	return validateSyncStatus("service router", router.Status)
}

func overTwoSeconds(t1, t2 time.Time) bool {
	return t1.Sub(t2).Abs() > 2*time.Second
}
