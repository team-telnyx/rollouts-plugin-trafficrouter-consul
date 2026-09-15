// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: Apache-2.0

package plugin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/argoproj/argo-rollouts/pkg/apis/rollouts/v1alpha1"
	consulv1aplha1 "github.com/hashicorp/consul-k8s/control-plane/api/v1alpha1"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestSetWeight(t *testing.T) {
	testCases := []struct {
		testName         string
		rollout          *v1alpha1.Rollout
		desiredWeight    int32
		inputResolver    *consulv1aplha1.ServiceResolver
		inputSplitter    *consulv1aplha1.ServiceSplitter
		expectedResolver *consulv1aplha1.ServiceResolver
		expectedSplitter *consulv1aplha1.ServiceSplitter
		expectedError    string
	}{
		{
			testName: "in progress, desired weight 50",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: defaultResolver(),
			inputSplitter: defaultSplitter(),
			expectedResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == 1",
						},
						"canary": {
							Filter: "Service.Meta.version == \"2\"",
						},
					},
				},
			},
			expectedSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        50,
							ServiceSubset: "canary",
						},
						{
							Weight:        50,
							ServiceSubset: "stable",
						},
					},
				},
			},
		},
		{
			testName: "in progress, desired weight 25",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 25,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 75,
							},
						},
					},
				},
			},
			desiredWeight: 25,
			inputResolver: defaultResolver(),
			inputSplitter: defaultSplitter(),
			expectedResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == 1",
						},
						"canary": {
							Filter: "Service.Meta.version == \"2\"",
						},
					},
				},
			},
			expectedSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        25,
							ServiceSubset: "canary",
						},
						{
							Weight:        75,
							ServiceSubset: "stable",
						},
					},
				},
			},
		},
		{
			testName: "in progress, desired weight 75",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 75,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 25,
							},
						},
					},
				},
			},
			desiredWeight: 75,
			inputResolver: defaultResolver(),
			inputSplitter: defaultSplitter(),
			expectedResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == 1",
						},
						"canary": {
							Filter: "Service.Meta.version == \"2\"",
						},
					},
				},
			},
			expectedSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        75,
							ServiceSubset: "canary",
						},
						{
							Weight:        25,
							ServiceSubset: "stable",
						},
					},
				},
			},
		},
		{
			testName: "in progress, desired weight 0",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 0,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 100,
							},
						},
					},
				},
			},
			desiredWeight: 0,
			inputResolver: defaultResolver(),
			inputSplitter: defaultSplitter(),
			expectedResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == 1",
						},
						"canary": {
							Filter: "Service.Meta.version == \"2\"",
						},
					},
				},
			},
			expectedSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        0,
							ServiceSubset: "canary",
						},
						{
							Weight:        100,
							ServiceSubset: "stable",
						},
					},
				},
			},
		},
		{
			testName: "completed, desired weight 0",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionTrue,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 0,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 100,
							},
						},
					},
				},
			},
			desiredWeight: 0,
			inputResolver: defaultResolver(),
			inputSplitter: defaultSplitter(),
			expectedResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == \"2\"",
						},
						"canary": {
							Filter: "",
						},
					},
				},
			},
			expectedSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        0,
							ServiceSubset: "canary",
						},
						{
							Weight:        100,
							ServiceSubset: "stable",
						},
					},
				},
			},
		},
		{
			testName: "in progress, desired weight 100",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 100,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 0,
							},
						},
					},
				},
			},
			desiredWeight: 100,
			inputResolver: defaultResolver(),
			inputSplitter: defaultSplitter(),
			expectedResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == 1",
						},
						"canary": {
							Filter: "Service.Meta.version == \"2\"",
						},
					},
				},
			},
			expectedSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        100,
							ServiceSubset: "canary",
						},
						{
							Weight:        0,
							ServiceSubset: "stable",
						},
					},
				},
			},
		},
		{
			testName: "aborted rollout",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Abort:              true,
					AbortedAt:          &metav1.Time{Time: time.Now()},
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 100,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 0,
							},
						},
					},
				},
			},
			desiredWeight: 0,
			inputResolver: defaultResolver(),
			inputSplitter: defaultSplitter(),
			expectedResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == 1",
						},
						"canary": {
							Filter: "",
						},
					},
				},
			},
			expectedSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        0,
							ServiceSubset: "canary",
						},
						{
							Weight:        100,
							ServiceSubset: "stable",
						},
					},
				},
			},
		},
		{
			testName: "in progress, desired weight 50, non-default-suffix",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-number": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJsonWithSuffix("number"),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.number == 1",
						},
						"canary": {
							Filter: "",
						},
					},
				},
			},
			inputSplitter: defaultSplitter(),
			expectedResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.number == 1",
						},
						"canary": {
							Filter: "Service.Meta.number == \"2\"",
						},
					},
				},
			},
			expectedSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        50,
							ServiceSubset: "canary",
						},
						{
							Weight:        50,
							ServiceSubset: "stable",
						},
					},
				},
			},
		},
		{
			testName: "empty canary status returns immediately",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-number": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJsonWithSuffix("number"),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{},
				},
			},
			desiredWeight:    50,
			inputResolver:    defaultResolver(),
			inputSplitter:    defaultSplitter(),
			expectedResolver: defaultResolver(),
			expectedSplitter: defaultSplitter(),
		},
		{
			testName: "error invalid rollout config",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-number": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: invalidPlugin(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight:    50,
			inputResolver:    defaultResolver(),
			inputSplitter:    defaultSplitter(),
			expectedResolver: nil,
			expectedSplitter: nil,
			expectedError:    "invalid consul traffic routing configuration.",
		},
		{
			testName: "error missing resolver",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight:    50,
			inputResolver:    nil,
			inputSplitter:    defaultSplitter(),
			expectedResolver: nil,
			expectedSplitter: nil,
			expectedError:    "serviceresolvers.consul.hashicorp.com \"test-service\" not found",
		},
		{
			testName: "error missing splitter",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight:    50,
			inputResolver:    defaultResolver(),
			inputSplitter:    nil,
			expectedResolver: nil,
			expectedSplitter: nil,
			expectedError:    "servicesplitters.consul.hashicorp.com \"test-service\" not found",
		},
		{
			testName: "error in progress rollout invalid resolver",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"foo": {
							Filter: "Service.Meta.version == 1",
						},
						"bar": {
							Filter: "",
						},
					},
				},
			},
			inputSplitter: defaultSplitter(),
			expectedError: "spec.subsets.canary.filter was not found in consul service resolver",
		},
		{
			testName: "error aborted rollout invalid resolver",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Abort:              true,
					AbortedAt:          &metav1.Time{Time: time.Now()},
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 100,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 0,
							},
						},
					},
				},
			},
			desiredWeight: 0,
			inputResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"foo": {
							Filter: "Service.Meta.version == 1",
						},
						"bar": {
							Filter: "",
						},
					},
				},
			},
			inputSplitter: defaultSplitter(),
			expectedError: "spec.subsets.canary.filter was not found in consul service resolver",
		},
		{
			testName: "error completed rollout invalid resolver invalid canary subset",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionTrue,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 0,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 100,
							},
						},
					},
				},
			},
			desiredWeight: 0,
			inputResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"foo": {
							Filter: "Service.Meta.version == 1",
						},
						"stable": {
							Filter: "",
						},
					},
				},
			},
			inputSplitter: defaultSplitter(),
			expectedError: "spec.subsets.canary.filter was not found in consul service resolver",
		},
		{
			testName: "error completed rollout invalid resolver invalid stable subset",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionTrue,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 0,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 100,
							},
						},
					},
				},
			},
			desiredWeight: 0,
			inputResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"canary": {
							Filter: "Service.Meta.version == 1",
						},
						"bar": {
							Filter: "",
						},
					},
				},
			},
			inputSplitter: defaultSplitter(),
			expectedError: "spec.subsets.stable.filter was not found in consul service resolver",
		},
		{
			testName: "error missing splitter subsets",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: defaultResolver(),
			inputSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{},
				},
			},
			expectedError: "spec.splits was not found in consul service splitter",
		},
		{
			testName: "error invalid number of splitter subsets",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: defaultResolver(),
			inputSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        100,
							ServiceSubset: "stable",
						},
						{
							Weight:        0,
							ServiceSubset: "canary",
						},
						{
							Weight:        0,
							ServiceSubset: "other",
						},
					},
				},
			},
			expectedError: "unexpected number of service splits. Expected 2, found 3",
		},
		{
			testName: "error invalid splitter subset names",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: defaultResolver(),
			inputSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        100,
							ServiceSubset: "foo",
						},
						{
							Weight:        0,
							ServiceSubset: "bar",
						},
					},
				},
			},
			expectedError: "unexpected service split",
		},
		{
			testName: "error invalid resolver status not synced",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == 1",
						},
						"canary": {
							Filter: "",
						},
					},
				},
				Status: consulv1aplha1.Status{
					Conditions: []consulv1aplha1.Condition{
						{
							Type:   consulv1aplha1.ConditionSynced,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			inputSplitter: defaultSplitter(),
			expectedError: "service resolver has not synced with Consul. The service resolver needs to be up to date before rollout can continue",
		},
		{
			testName: "error invalid resolver last synced time mismatch",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: &consulv1aplha1.ServiceResolver{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceResolverSpec{
					Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
						"stable": {
							Filter: "Service.Meta.version == 1",
						},
						"canary": {
							Filter: "",
						},
					},
				},
				Status: consulv1aplha1.Status{
					Conditions: []consulv1aplha1.Condition{
						{
							Type:               consulv1aplha1.ConditionSynced,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: unknownSyncTime(t)},
						},
					},
					LastSyncedTime: &metav1.Time{Time: time.Now()},
				},
			},
			inputSplitter: defaultSplitter(),
			expectedError: "service resolver has not synced with Consul. The service resolver needs to be up to date before rollout can continue",
		},
		{
			testName: "error invalid splitter status not synced",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: defaultResolver(),
			inputSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        100,
							ServiceSubset: "stable",
						},
						{
							Weight:        0,
							ServiceSubset: "canary",
						},
					},
				},
				Status: consulv1aplha1.Status{
					Conditions: []consulv1aplha1.Condition{
						{
							Type:   consulv1aplha1.ConditionSynced,
							Status: corev1.ConditionFalse,
						},
					},
				},
			},
			expectedError: "service splitter has not synced with Consul. The service splitter needs to be up to date before rollout can continue",
		},
		{
			testName: "error invalid splitter last synced time mismatch",
			rollout: &v1alpha1.Rollout{
				ObjectMeta: metav1.ObjectMeta{
					Name:       "rollout",
					Namespace:  "default",
					Generation: 10,
				},
				Spec: v1alpha1.RolloutSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"consul.hashicorp.com/service-meta-version": "2",
							},
						},
					},
					Strategy: v1alpha1.RolloutStrategy{
						Canary: &v1alpha1.CanaryStrategy{
							TrafficRouting: &v1alpha1.RolloutTrafficRouting{
								Plugins: map[string]json.RawMessage{
									ConfigKey: pluginJson(),
								},
							},
						},
					},
				},
				Status: v1alpha1.RolloutStatus{
					ObservedGeneration: "10",
					Conditions: []v1alpha1.RolloutCondition{
						{
							Type:   v1alpha1.RolloutCompleted,
							Status: corev1.ConditionFalse,
						},
					},
					Canary: v1alpha1.CanaryStatus{
						Weights: &v1alpha1.TrafficWeights{
							Canary: v1alpha1.WeightDestination{
								Weight: 50,
							},
							Stable: v1alpha1.WeightDestination{
								Weight: 50,
							},
						},
					},
				},
			},
			desiredWeight: 50,
			inputResolver: defaultResolver(),
			inputSplitter: &consulv1aplha1.ServiceSplitter{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-service",
					Namespace: "default",
				},
				Spec: consulv1aplha1.ServiceSplitterSpec{
					Splits: []consulv1aplha1.ServiceSplit{
						{
							Weight:        100,
							ServiceSubset: "stable",
						},
						{
							Weight:        0,
							ServiceSubset: "canary",
						},
					},
				},
				Status: consulv1aplha1.Status{
					Conditions: []consulv1aplha1.Condition{
						{
							Type:               consulv1aplha1.ConditionSynced,
							Status:             corev1.ConditionTrue,
							LastTransitionTime: metav1.Time{Time: unknownSyncTime(t)},
						},
					},
					LastSyncedTime: &metav1.Time{Time: time.Now()},
				},
			},
			expectedError: "service splitter has not synced with Consul. The service splitter needs to be up to date before rollout can continue",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(t *testing.T) {
			s := runtime.NewScheme()
			require.NoError(t, consulv1aplha1.AddToScheme(s))

			objs := []client.Object{}

			if testCase.inputResolver != nil {
				objs = append(objs, testCase.inputResolver)
			}
			if testCase.inputSplitter != nil {
				objs = append(objs, testCase.inputSplitter)
			}

			namespacedName := types.NamespacedName{Name: "test-service", Namespace: "default"}

			k8sClient := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
			p := &RpcPlugin{
				K8SClient: k8sClient,
				IsTest:    true,
				LogCtx:    logrus.NewEntry(logrus.New()),
			}
			err := p.SetWeight(testCase.rollout, testCase.desiredWeight, []v1alpha1.WeightDestination{})
			if testCase.expectedError == "" {
				actualResolver := &consulv1aplha1.ServiceResolver{}
				actualSplitter := &consulv1aplha1.ServiceSplitter{}
				require.NoError(t, k8sClient.Get(context.TODO(), namespacedName, actualResolver, &client.GetOptions{}))
				require.NoError(t, k8sClient.Get(context.TODO(), namespacedName, actualSplitter, &client.GetOptions{}))
				require.ElementsMatch(t, testCase.expectedSplitter.Spec.Splits, actualSplitter.Spec.Splits)
				require.Equal(t, testCase.expectedResolver.Spec.Subsets["canary"], actualResolver.Spec.Subsets["canary"])
				require.Equal(t, testCase.expectedResolver.Spec.Subsets["stable"], actualResolver.Spec.Subsets["stable"])
			} else {
				require.Contains(t, err.ErrorString, testCase.expectedError)
			}
		})
	}
}

func TestRpcPluginType(t *testing.T) {
	p := &RpcPlugin{}
	require.Equal(t, Type, p.Type())
}

func pluginJson() []byte {
	config := ConsulTrafficRouting{
		ServiceName:      "test-service",
		CanarySubsetName: "canary",
		StableSubsetName: "stable",
	}
	jsonConfig, _ := json.Marshal(config)
	return jsonConfig
}

func pluginJsonWithSuffix(suffix string) []byte {
	config := ConsulTrafficRouting{
		ServiceName:                 "test-service",
		CanarySubsetName:            "canary",
		StableSubsetName:            "stable",
		ServiceMetaAnnotationSuffix: suffix,
	}
	jsonConfig, _ := json.Marshal(config)
	return jsonConfig
}

func invalidPlugin() []byte {
	config := struct{}{}
	jsonConfig, _ := json.Marshal(config)
	return jsonConfig
}

func defaultSplitter() *consulv1aplha1.ServiceSplitter {
	return &consulv1aplha1.ServiceSplitter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: consulv1aplha1.ServiceSplitterSpec{
			Splits: []consulv1aplha1.ServiceSplit{
				{
					Weight:        100,
					ServiceSubset: "stable",
				},
				{
					Weight:        0,
					ServiceSubset: "canary",
				},
			},
		},
		Status: consulv1aplha1.Status{
			Conditions: []consulv1aplha1.Condition{
				{
					Type:               consulv1aplha1.ConditionSynced,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
			},
			LastSyncedTime: &metav1.Time{Time: time.Now()},
		},
	}
}

func defaultResolver() *consulv1aplha1.ServiceResolver {
	return &consulv1aplha1.ServiceResolver{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Spec: consulv1aplha1.ServiceResolverSpec{
			Subsets: map[string]consulv1aplha1.ServiceResolverSubset{
				"stable": {
					Filter: "Service.Meta.version == 1",
				},
				"canary": {
					Filter: "",
				},
			},
		},
		Status: consulv1aplha1.Status{
			Conditions: []consulv1aplha1.Condition{
				{
					Type:               consulv1aplha1.ConditionSynced,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
			},
			LastSyncedTime: &metav1.Time{Time: time.Now()},
		},
	}
}

func defaultRouter() *consulv1aplha1.ServiceRouter {
	return &consulv1aplha1.ServiceRouter{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-service",
			Namespace: "default",
		},
		Status: consulv1aplha1.Status{
			Conditions: []consulv1aplha1.Condition{
				{
					Type:               consulv1aplha1.ConditionSynced,
					Status:             corev1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: time.Now()},
				},
			},
			LastSyncedTime: &metav1.Time{Time: time.Now()},
		},
	}
}

func unknownSyncTime(t *testing.T) time.Time {
	const layout = "2006-01-02 15:04:05"
	timeString := "2023-03-06 08:30:00"
	parsedTime, err := time.Parse(layout, timeString)
	require.NoError(t, err)
	return parsedTime
}

func TestSetHeaderRoute(t *testing.T) {
	testCases := []struct {
		testName           string
		rollout            *v1alpha1.Rollout
		headerRoute        *v1alpha1.SetHeaderRoute
		inputRouter        *consulv1aplha1.ServiceRouter
		expectedRoutes     []consulv1aplha1.ServiceRoute
		expectedNames      []string
		expectedError      string
		expectNoWrite      bool
		expectRouterAbsent bool
	}{
		{
			// The single most important assertion in this suite: two match entries become ONE
			// consul route carrying TWO header matchers, because consul ANDs header matchers
			// within a route. The built-in istio reconciler would emit two routes and OR them.
			testName:    "no existing router, two matches produce one route with two header matchers",
			rollout:     headerRouteRollout(),
			headerRoute: headerRouteStep("canary-audience", regexMatch("user-agent", ".*Firefox.*"), exactMatch("x-geo-region", "africa")),
			inputRouter: nil,
			expectedRoutes: []consulv1aplha1.ServiceRoute{
				canaryRoute(
					consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "user-agent", Regex: ".*Firefox.*"},
					consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-geo-region", Exact: "africa"},
				),
			},
			expectedNames: []string{"canary-audience"},
		},
		{
			testName: "exact, prefix and regex each land in their own consul field",
			rollout:  headerRouteRollout(),
			headerRoute: headerRouteStep("mixed",
				exactMatch("x-user", "vip"),
				prefixMatch("x-tenant", "acme-"),
				regexMatch("user-agent", ".*Firefox.*"),
			),
			inputRouter: nil,
			expectedRoutes: []consulv1aplha1.ServiceRoute{
				canaryRoute(
					consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-user", Exact: "vip"},
					consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-tenant", Prefix: "acme-"},
					consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "user-agent", Regex: ".*Firefox.*"},
				),
			},
			expectedNames: []string{"mixed"},
		},
		{
			testName:    "a pre-existing foreign route is preserved and the managed route sits ahead of it",
			rollout:     headerRouteRollout(),
			headerRoute: headerRouteStep("canary-audience", exactMatch("x-canary", "true")),
			inputRouter: routerWithRoutes(foreignRoute()),
			expectedRoutes: []consulv1aplha1.ServiceRoute{
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-canary", Exact: "true"}),
				foreignRoute(),
			},
			expectedNames: []string{"canary-audience"},
		},
		{
			// Argo re-invokes SetHeaderRoute on every reconcile while the step is current.
			testName:    "the same route name is replaced in place and the route count is unchanged",
			rollout:     headerRouteRollout(),
			headerRoute: headerRouteStep("canary-audience", exactMatch("x-canary", "new")),
			inputRouter: managedRouter(t, []string{"canary-audience"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-canary", Exact: "old"}),
				foreignRoute(),
			),
			expectedRoutes: []consulv1aplha1.ServiceRoute{
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-canary", Exact: "new"}),
				foreignRoute(),
			},
			expectedNames: []string{"canary-audience"},
		},
		{
			testName:    "a different route name is appended after the existing managed routes",
			rollout:     headerRouteRollout(),
			headerRoute: headerRouteStep("second", exactMatch("x-two", "2")),
			inputRouter: managedRouter(t, []string{"first"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
				foreignRoute(),
			),
			expectedRoutes: []consulv1aplha1.ServiceRoute{
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-two", Exact: "2"}),
				foreignRoute(),
			},
			expectedNames: []string{"first", "second"},
		},
		{
			testName:    "a nil match removes that route and leaves the others untouched",
			rollout:     headerRouteRollout(),
			headerRoute: &v1alpha1.SetHeaderRoute{Name: "first"},
			inputRouter: managedRouter(t, []string{"first", "second"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-two", Exact: "2"}),
				foreignRoute(),
			),
			expectedRoutes: []consulv1aplha1.ServiceRoute{
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-two", Exact: "2"}),
				foreignRoute(),
			},
			expectedNames: []string{"second"},
		},
		{
			testName:    "removing the last managed route deletes a router the plugin created",
			rollout:     headerRouteRollout(),
			headerRoute: &v1alpha1.SetHeaderRoute{Name: "only"},
			inputRouter: managedRouter(t, []string{"only"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
			),
			expectRouterAbsent: true,
		},
		{
			testName:    "removing the last managed route empties but keeps a router the user created",
			rollout:     headerRouteRollout(),
			headerRoute: &v1alpha1.SetHeaderRoute{Name: "only"},
			inputRouter: notPluginCreated(managedRouter(t, []string{"only"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
			)),
			expectedRoutes: []consulv1aplha1.ServiceRoute{},
			expectedNames:  nil,
		},
		{
			testName:    "removing a route that is not managed performs no write",
			rollout:     headerRouteRollout(),
			headerRoute: &v1alpha1.SetHeaderRoute{Name: "never-created"},
			inputRouter: managedRouter(t, []string{"only"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
			),
			expectNoWrite: true,
		},
		{
			testName:           "removing a route when there is no router performs no write",
			rollout:            headerRouteRollout(),
			headerRoute:        &v1alpha1.SetHeaderRoute{Name: "never-created"},
			inputRouter:        nil,
			expectRouterAbsent: true,
		},
		{
			testName:           "a rollout without a canary status is a no-op",
			rollout:            noCanaryStatusRollout(),
			headerRoute:        headerRouteStep("canary-audience", exactMatch("x-canary", "true")),
			inputRouter:        nil,
			expectRouterAbsent: true,
		},
		{
			testName:      "a rollout without a canary status does not write to an existing router",
			rollout:       noCanaryStatusRollout(),
			headerRoute:   headerRouteStep("canary-audience", exactMatch("x-canary", "true")),
			inputRouter:   routerWithRoutes(foreignRoute()),
			expectNoWrite: true,
		},
		{
			testName:           "an invalid plugin config is an error",
			rollout:            rolloutWithPluginConfig(invalidPlugin()),
			headerRoute:        headerRouteStep("canary-audience", exactMatch("x-canary", "true")),
			inputRouter:        nil,
			expectedError:      "invalid consul traffic routing configuration",
			expectRouterAbsent: true,
		},
		{
			testName:      "a router that has not synced with consul is an error",
			rollout:       headerRouteRollout(),
			headerRoute:   headerRouteStep("canary-audience", exactMatch("x-canary", "true")),
			inputRouter:   unsyncedRouter(t),
			expectedError: "service router has not synced with Consul",
			expectNoWrite: true,
		},
		{
			testName:    "a match without a header value is rejected before any write",
			rollout:     headerRouteRollout(),
			headerRoute: &v1alpha1.SetHeaderRoute{Name: "bad", Match: []v1alpha1.HeaderRoutingMatch{{HeaderName: "x-canary"}}},
			inputRouter: routerWithRoutes(foreignRoute()),

			expectedError: "headerValue must be set for header \"x-canary\"",
			expectNoWrite: true,
		},
		{
			// consul-k8s rejects a header match with more than one of exact/prefix/suffix/regex/present.
			testName: "a match setting both exact and regex is rejected before any write",
			rollout:  headerRouteRollout(),
			headerRoute: &v1alpha1.SetHeaderRoute{Name: "bad", Match: []v1alpha1.HeaderRoutingMatch{{
				HeaderName:  "x-canary",
				HeaderValue: &v1alpha1.StringMatch{Exact: "true", Regex: ".*"},
			}}},
			inputRouter:   routerWithRoutes(foreignRoute()),
			expectedError: "sets more than one of exact, prefix or regex",
			expectNoWrite: true,
		},
		{
			testName: "an annotation claiming more managed routes than exist is an error",
			rollout:  headerRouteRollout(),

			headerRoute:   headerRouteStep("canary-audience", exactMatch("x-canary", "true")),
			inputRouter:   withRawManagedNames(managedRouter(t, []string{"a"}, canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x", Exact: "1"})), `["a","b"]`),
			expectedError: "refusing to guess which routes are managed",
			expectNoWrite: true,
		},
		{
			// The managed prefix must actually point at the canary subset. If it does not, the
			// annotation has desynced from spec.routes and blind indexing would strip somebody
			// else's route.
			testName:      "a managed prefix that does not target the canary subset is an error",
			rollout:       headerRouteRollout(),
			headerRoute:   headerRouteStep("canary-audience", exactMatch("x-canary", "true")),
			inputRouter:   managedRouter(t, []string{"a"}, foreignRoute()),
			expectedError: "does not target the canary subset",
			expectNoWrite: true,
		},
		{
			testName:    "a router managed by another rollout is not written to",
			rollout:     headerRouteRollout(),
			headerRoute: headerRouteStep("canary-audience", exactMatch("x-canary", "true")),
			inputRouter: ownedBy(managedRouter(t, []string{"a"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x", Exact: "1"})), "default/other-rollout"),
			expectedError: "already has header routes managed by rollout \"default/other-rollout\"",
			expectNoWrite: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(t *testing.T) {
			k8sClient, before := newRouterClient(t, testCase.inputRouter)

			p := &RpcPlugin{
				K8SClient: k8sClient,
				IsTest:    true,
				LogCtx:    logrus.NewEntry(logrus.New()),
			}
			err := p.SetHeaderRoute(testCase.rollout, testCase.headerRoute)

			if testCase.expectedError == "" {
				require.Empty(t, err.ErrorString)
			} else {
				require.Contains(t, err.ErrorString, testCase.expectedError)
			}

			assertRouter(t, k8sClient, before, routerExpectation{
				routes:     testCase.expectedRoutes,
				names:      testCase.expectedNames,
				noWrite:    testCase.expectNoWrite,
				absent:     testCase.expectRouterAbsent,
				hadRouter:  testCase.inputRouter != nil,
				pluginMade: testCase.inputRouter == nil,
			})
		})
	}
}

func TestRemoveManagedRoutes(t *testing.T) {
	testCases := []struct {
		testName           string
		rollout            *v1alpha1.Rollout
		inputRouter        *consulv1aplha1.ServiceRouter
		expectedRoutes     []consulv1aplha1.ServiceRoute
		expectedNames      []string
		expectedError      string
		expectNoWrite      bool
		expectRouterAbsent bool
	}{
		{
			testName:           "an absent router is a no-op",
			rollout:            headerRouteRollout(),
			inputRouter:        nil,
			expectRouterAbsent: true,
		},
		{
			// RemoveManagedRoutes runs on every reconcile of every fully promoted rollout. A
			// router with no managed-routes annotation must not be written to at all, which this
			// asserts by way of an unchanged ResourceVersion.
			testName:      "a router with no managed-routes annotation is not written at all",
			rollout:       headerRouteRollout(),
			inputRouter:   routerWithRoutes(foreignRoute()),
			expectNoWrite: true,
		},
		{
			testName: "a plugin-created router holding only managed routes is deleted",
			rollout:  headerRouteRollout(),
			inputRouter: managedRouter(t, []string{"first", "second"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-two", Exact: "2"}),
			),
			expectRouterAbsent: true,
		},
		{
			testName: "a foreign route survives and the router is kept",
			rollout:  headerRouteRollout(),
			inputRouter: managedRouter(t, []string{"first"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
				foreignRoute(),
			),
			expectedRoutes: []consulv1aplha1.ServiceRoute{foreignRoute()},
			expectedNames:  nil,
		},
		{
			testName: "a router the user created is emptied but not deleted",
			rollout:  headerRouteRollout(),
			inputRouter: notPluginCreated(managedRouter(t, []string{"first"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"}),
			)),
			expectedRoutes: []consulv1aplha1.ServiceRoute{},
			expectedNames:  nil,
		},
		{
			testName: "routes managed by another rollout are left alone without a write",
			rollout:  headerRouteRollout(),
			inputRouter: ownedBy(managedRouter(t, []string{"first"},
				canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x-one", Exact: "1"})), "default/other-rollout"),
			expectNoWrite: true,
		},
		{
			testName:      "a desynced annotation is an error and nothing is written",
			rollout:       headerRouteRollout(),
			inputRouter:   withRawManagedNames(managedRouter(t, []string{"a"}, canaryRoute(consulv1aplha1.ServiceRouteHTTPMatchHeader{Name: "x", Exact: "1"})), `["a","b"]`),
			expectedError: "refusing to guess which routes are managed",
			expectNoWrite: true,
		},
		{
			testName:           "an invalid plugin config is an error",
			rollout:            rolloutWithPluginConfig(invalidPlugin()),
			inputRouter:        nil,
			expectedError:      "invalid consul traffic routing configuration",
			expectRouterAbsent: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.testName, func(t *testing.T) {
			k8sClient, before := newRouterClient(t, testCase.inputRouter)

			p := &RpcPlugin{
				K8SClient: k8sClient,
				IsTest:    true,
				LogCtx:    logrus.NewEntry(logrus.New()),
			}
			err := p.RemoveManagedRoutes(testCase.rollout)

			if testCase.expectedError == "" {
				require.Empty(t, err.ErrorString)
			} else {
				require.Contains(t, err.ErrorString, testCase.expectedError)
			}

			assertRouter(t, k8sClient, before, routerExpectation{
				routes:    testCase.expectedRoutes,
				names:     testCase.expectedNames,
				noWrite:   testCase.expectNoWrite,
				absent:    testCase.expectRouterAbsent,
				hadRouter: testCase.inputRouter != nil,
			})

			// Every path must be idempotent: a second call is a no-op and, crucially, performs
			// no write. This is the state Argo leaves every promoted rollout in forever.
			settled := routerResourceVersion(t, k8sClient)
			secondErr := p.RemoveManagedRoutes(testCase.rollout)
			if testCase.expectedError == "" {
				require.Empty(t, secondErr.ErrorString, "second RemoveManagedRoutes call should succeed")
			}
			require.Equal(t, settled, routerResourceVersion(t, k8sClient),
				"second RemoveManagedRoutes call must not write to the ServiceRouter")
		})
	}
}

// TestValidateSyncStatusNilLastSyncedTime pins the nil-deref fix: a resource carrying a Synced
// condition but no lastSyncedTime used to panic the plugin, because Status.LastSyncedTime is a
// *metav1.Time that is nil until consul-k8s has synced the resource at least once.
func TestValidateSyncStatusNilLastSyncedTime(t *testing.T) {
	status := consulv1aplha1.Status{
		Conditions: []consulv1aplha1.Condition{
			{
				Type:               consulv1aplha1.ConditionSynced,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Time{Time: time.Now()},
			},
		},
		LastSyncedTime: nil,
	}

	require.NotPanics(t, func() {
		require.Error(t, validateResolverSyncStatus(&consulv1aplha1.ServiceResolver{Status: status}))
		require.Error(t, validateSplitterSyncStatus(&consulv1aplha1.ServiceSplitter{Status: status}))
		require.Error(t, validateRouterSyncStatus(&consulv1aplha1.ServiceRouter{Status: status}))
	})
}

type routerExpectation struct {
	routes     []consulv1aplha1.ServiceRoute
	names      []string
	noWrite    bool
	absent     bool
	hadRouter  bool
	pluginMade bool
}

// newRouterClient builds a fake client seeded with router (if any) and returns the client along
// with the router's ResourceVersion, so a test can assert that no write took place.
func newRouterClient(t *testing.T, router *consulv1aplha1.ServiceRouter) (client.Client, string) {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, consulv1aplha1.AddToScheme(s))

	objs := []client.Object{}
	if router != nil {
		objs = append(objs, router)
	}
	k8sClient := fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
	return k8sClient, routerResourceVersion(t, k8sClient)
}

// routerResourceVersion returns the ServiceRouter's ResourceVersion, or "" when it is absent.
func routerResourceVersion(t *testing.T, k8sClient client.Client) string {
	t.Helper()
	actual := &consulv1aplha1.ServiceRouter{}
	err := k8sClient.Get(context.TODO(), routerNamespacedName(), actual, &client.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ""
	}
	require.NoError(t, err)
	return actual.GetResourceVersion()
}

func routerNamespacedName() types.NamespacedName {
	return types.NamespacedName{Name: "test-service", Namespace: "default"}
}

func assertRouter(t *testing.T, k8sClient client.Client, before string, want routerExpectation) {
	t.Helper()

	actual := &consulv1aplha1.ServiceRouter{}
	err := k8sClient.Get(context.TODO(), routerNamespacedName(), actual, &client.GetOptions{})

	if want.absent {
		require.True(t, apierrors.IsNotFound(err), "expected no ServiceRouter to exist, got err=%v", err)
		return
	}
	require.NoError(t, err)

	if want.noWrite {
		require.Equal(t, before, actual.GetResourceVersion(), "expected no write to the ServiceRouter")
		return
	}
	require.NotEqual(t, before, actual.GetResourceVersion(), "expected the ServiceRouter to have been written")

	if len(want.routes) == 0 {
		require.Empty(t, actual.Spec.Routes)
	} else {
		require.Equal(t, want.routes, actual.Spec.Routes)
	}
	require.Equal(t, want.names, managedNamesFromAnnotation(t, actual))

	if want.pluginMade {
		require.True(t, routerCreatedByPlugin(actual), "expected the router to be marked as created by the plugin")
	}
	if len(want.names) > 0 {
		require.Equal(t, "default/rollout", actual.GetAnnotations()[managedByAnnotation])
	} else {
		require.NotContains(t, actual.GetAnnotations(), managedByAnnotation)
	}
}

func managedNamesFromAnnotation(t *testing.T, router *consulv1aplha1.ServiceRouter) []string {
	t.Helper()
	raw := router.GetAnnotations()[managedRoutesAnnotation]
	if raw == "" {
		return nil
	}
	var names []string
	require.NoError(t, json.Unmarshal([]byte(raw), &names))
	return names
}

func headerRouteStep(name string, matches ...v1alpha1.HeaderRoutingMatch) *v1alpha1.SetHeaderRoute {
	return &v1alpha1.SetHeaderRoute{Name: name, Match: matches}
}

func exactMatch(name, value string) v1alpha1.HeaderRoutingMatch {
	return v1alpha1.HeaderRoutingMatch{HeaderName: name, HeaderValue: &v1alpha1.StringMatch{Exact: value}}
}

func prefixMatch(name, value string) v1alpha1.HeaderRoutingMatch {
	return v1alpha1.HeaderRoutingMatch{HeaderName: name, HeaderValue: &v1alpha1.StringMatch{Prefix: value}}
}

func regexMatch(name, value string) v1alpha1.HeaderRoutingMatch {
	return v1alpha1.HeaderRoutingMatch{HeaderName: name, HeaderValue: &v1alpha1.StringMatch{Regex: value}}
}

// canaryRoute is what the plugin is expected to write: one route, every header matcher ANDed
// into it, destined for the canary subset.
func canaryRoute(headers ...consulv1aplha1.ServiceRouteHTTPMatchHeader) consulv1aplha1.ServiceRoute {
	return consulv1aplha1.ServiceRoute{
		Match: &consulv1aplha1.ServiceRouteMatch{
			HTTP: &consulv1aplha1.ServiceRouteHTTPMatch{Header: headers},
		},
		Destination: &consulv1aplha1.ServiceRouteDestination{
			Service:       "test-service",
			ServiceSubset: "canary",
		},
	}
}

// foreignRoute is a route the plugin did not write and must never touch.
func foreignRoute() consulv1aplha1.ServiceRoute {
	return consulv1aplha1.ServiceRoute{
		Match: &consulv1aplha1.ServiceRouteMatch{
			HTTP: &consulv1aplha1.ServiceRouteHTTPMatch{PathPrefix: "/admin"},
		},
		Destination: &consulv1aplha1.ServiceRouteDestination{
			Service:       "test-service",
			ServiceSubset: "stable",
		},
	}
}

func routerWithRoutes(routes ...consulv1aplha1.ServiceRoute) *consulv1aplha1.ServiceRouter {
	router := defaultRouter()
	router.Spec.Routes = routes
	return router
}

// managedRouter is a ServiceRouter the plugin created, whose first len(names) routes are the
// plugin's own.
func managedRouter(t *testing.T, names []string, routes ...consulv1aplha1.ServiceRoute) *consulv1aplha1.ServiceRouter {
	t.Helper()
	router := routerWithRoutes(routes...)
	encoded, err := json.Marshal(names)
	require.NoError(t, err)
	router.SetAnnotations(map[string]string{
		managedRoutesAnnotation:         string(encoded),
		managedByAnnotation:             "default/rollout",
		routerCreatedByPluginAnnotation: "true",
	})
	return router
}

func notPluginCreated(router *consulv1aplha1.ServiceRouter) *consulv1aplha1.ServiceRouter {
	annotations := router.GetAnnotations()
	delete(annotations, routerCreatedByPluginAnnotation)
	router.SetAnnotations(annotations)
	return router
}

func ownedBy(router *consulv1aplha1.ServiceRouter, owner string) *consulv1aplha1.ServiceRouter {
	annotations := router.GetAnnotations()
	annotations[managedByAnnotation] = owner
	router.SetAnnotations(annotations)
	return router
}

func withRawManagedNames(router *consulv1aplha1.ServiceRouter, raw string) *consulv1aplha1.ServiceRouter {
	annotations := router.GetAnnotations()
	annotations[managedRoutesAnnotation] = raw
	router.SetAnnotations(annotations)
	return router
}

func unsyncedRouter(t *testing.T) *consulv1aplha1.ServiceRouter {
	t.Helper()
	router := routerWithRoutes(foreignRoute())
	router.Status.Conditions[0].LastTransitionTime = metav1.Time{Time: unknownSyncTime(t)}
	return router
}

func rolloutWithPluginConfig(config []byte) *v1alpha1.Rollout {
	rollout := headerRouteRollout()
	rollout.Spec.Strategy.Canary.TrafficRouting.Plugins[ConfigKey] = config
	return rollout
}

func noCanaryStatusRollout() *v1alpha1.Rollout {
	rollout := headerRouteRollout()
	rollout.Status.Canary = v1alpha1.CanaryStatus{}
	return rollout
}

func headerRouteRollout() *v1alpha1.Rollout {
	return &v1alpha1.Rollout{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "rollout",
			Namespace:  "default",
			Generation: 10,
		},
		Spec: v1alpha1.RolloutSpec{
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"consul.hashicorp.com/service-meta-version": "2",
					},
				},
			},
			Strategy: v1alpha1.RolloutStrategy{
				Canary: &v1alpha1.CanaryStrategy{
					TrafficRouting: &v1alpha1.RolloutTrafficRouting{
						Plugins: map[string]json.RawMessage{
							ConfigKey: pluginJson(),
						},
					},
				},
			},
		},
		Status: v1alpha1.RolloutStatus{
			ObservedGeneration: "10",
			Conditions: []v1alpha1.RolloutCondition{
				{
					Type:   v1alpha1.RolloutCompleted,
					Status: corev1.ConditionFalse,
				},
			},
			Canary: v1alpha1.CanaryStatus{
				Weights: &v1alpha1.TrafficWeights{
					Canary: v1alpha1.WeightDestination{Weight: 50},
					Stable: v1alpha1.WeightDestination{Weight: 50},
				},
			},
		},
	}
}
