// Copyright © 2019 Banzai Cloud
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package model

import (
	"context"
	"os"
	"sort"

	"emperror.dev/errors"
	"github.com/cisco-open/operator-tools/pkg/utils"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kube-logging/logging-operator/pkg/sdk/logging/api/v1beta1"
)

func NewLoggingResourceRepository(client client.Reader, logger logr.Logger) *LoggingResourceRepository {
	return &LoggingResourceRepository{
		Client: client,
		Logger: logger,
	}
}

type LoggingResourceRepository struct {
	Client client.Reader
	Logger logr.Logger
}

func (r LoggingResourceRepository) LoggingResourcesFor(ctx context.Context, logging v1beta1.Logging) (res LoggingResources, errs error) {
	res.Logging = logging

	var err error

	allLoggings := &v1beta1.LoggingList{}
	if err := r.Client.List(ctx, allLoggings); err != nil {
		errs = errors.Append(errs, err)
	}
	res.AllLoggings = allLoggings.Items

	res.Fluentd.ClusterFlows, err = r.ClusterFlowsFor(ctx, logging)
	errs = errors.Append(errs, err)

	res.Fluentd.ClusterOutputs, err = r.ClusterOutputsFor(ctx, logging)
	errs = errors.Append(errs, err)

	res.Fluentd.Configuration, err = r.FluentdConfigFor(ctx, &logging)
	errs = errors.Append(errs, err)

	res.SyslogNG.ClusterFlows, err = r.SyslogNGClusterFlowsFor(ctx, logging)
	errs = errors.Append(errs, err)

	res.SyslogNG.ClusterOutputs, err = r.SyslogNGClusterOutputsFor(ctx, logging)
	errs = errors.Append(errs, err)

	res.NodeAgents, err = r.NodeAgentsFor(ctx, logging)
	errs = errors.Append(errs, err)

	res.Fluentbits, err = r.FluentbitsFor(ctx, logging)
	errs = errors.Append(errs, err)

	res.LoggingRoutes, err = r.LoggingRoutesFor(ctx, logging)
	errs = errors.Append(errs, err)

	res.WatchNamespaces, err = UniqueWatchNamespaces(ctx, r.Client, &logging)
	if err != nil {
		errs = errors.Append(errs, err)
		return
	}

	for _, ns := range res.WatchNamespaces {
		{
			flows, err := r.FlowsInNamespaceFor(ctx, ns, logging)
			res.Fluentd.Flows = append(res.Fluentd.Flows, flows...)
			errs = errors.Append(errs, err)
		}

		{
			outputs, err := r.OutputsInNamespaceFor(ctx, ns, logging)
			res.Fluentd.Outputs = append(res.Fluentd.Outputs, outputs...)
			errs = errors.Append(errs, err)
		}

		{
			flows, err := r.SyslogNGFlowsInNamespaceFor(ctx, ns, logging)
			res.SyslogNG.Flows = append(res.SyslogNG.Flows, flows...)
			errs = errors.Append(errs, err)
		}

		{
			outputs, err := r.SyslogNGOutputsInNamespaceFor(ctx, ns, logging)
			res.SyslogNG.Outputs = append(res.SyslogNG.Outputs, outputs...)
			errs = errors.Append(errs, err)
		}
	}

	return
}

func UniqueWatchNamespaces(ctx context.Context, reader client.Reader, logging *v1beta1.Logging) ([]string, error) {
	watchNamespaces := logging.Spec.WatchNamespaces
	nsLabelSelector := logging.Spec.WatchNamespaceSelector
	if len(watchNamespaces) == 0 || nsLabelSelector != nil {
		var nsList corev1.NamespaceList
		var nsListOptions = &client.ListOptions{}
		if nsLabelSelector != nil {
			selector, err := metav1.LabelSelectorAsSelector(nsLabelSelector)
			if err != nil {
				return nil, errors.WrapIf(err, "error in watchNamespaceSelector")
			}
			nsListOptions = &client.ListOptions{
				LabelSelector: selector,
			}
		}
		if err := reader.List(ctx, &nsList, nsListOptions); err != nil {
			return nil, errors.WrapIf(err, "listing namespaces for watchNamespaceSelector")
		}
		for _, i := range nsList.Items {
			watchNamespaces = append(watchNamespaces, i.Name)
		}
	}
	uniqueWatchNamespaces := []string{}
	var previousNamespace string
	sort.Strings(watchNamespaces)

	for _, n := range watchNamespaces {
		if n != previousNamespace {
			uniqueWatchNamespaces = append(uniqueWatchNamespaces, n)
		}
		previousNamespace = n
	}
	return uniqueWatchNamespaces, nil
}

func (r LoggingResourceRepository) ClusterFlowsFor(ctx context.Context, logging v1beta1.Logging) ([]v1beta1.ClusterFlow, error) {
	var list v1beta1.ClusterFlowList
	if err := r.Client.List(ctx, &list, clusterResourceListOpts(logging)...); err != nil {
		return nil, err
	}

	var res []v1beta1.ClusterFlow
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}

	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})

	return res, nil
}

func (r LoggingResourceRepository) ClusterOutputsFor(ctx context.Context, logging v1beta1.Logging) ([]v1beta1.ClusterOutput, error) {
	var list v1beta1.ClusterOutputList
	if err := r.Client.List(ctx, &list, clusterResourceListOpts(logging)...); err != nil {
		return nil, err
	}

	var res []v1beta1.ClusterOutput
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}

	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})

	return res, nil
}

func (r LoggingResourceRepository) FlowsInNamespaceFor(ctx context.Context, namespace string, logging v1beta1.Logging) ([]v1beta1.Flow, error) {
	var list v1beta1.FlowList
	if err := r.Client.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	var res []v1beta1.Flow
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}

	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})

	return res, nil
}

func (r LoggingResourceRepository) OutputsInNamespaceFor(ctx context.Context, namespace string, logging v1beta1.Logging) ([]v1beta1.Output, error) {
	var list v1beta1.OutputList
	if err := r.Client.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	var res []v1beta1.Output
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}

	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})
	return res, nil
}

func (r LoggingResourceRepository) SyslogNGClusterFlowsFor(ctx context.Context, logging v1beta1.Logging) ([]v1beta1.SyslogNGClusterFlow, error) {
	var list v1beta1.SyslogNGClusterFlowList
	if err := r.Client.List(ctx, &list, clusterResourceListOpts(logging)...); err != nil {
		return nil, err
	}

	var res []v1beta1.SyslogNGClusterFlow
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}

	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})
	return res, nil
}

func (r LoggingResourceRepository) SyslogNGClusterOutputsFor(ctx context.Context, logging v1beta1.Logging) ([]v1beta1.SyslogNGClusterOutput, error) {
	var list v1beta1.SyslogNGClusterOutputList
	if err := r.Client.List(ctx, &list, clusterResourceListOpts(logging)...); err != nil {
		return nil, err
	}

	var res []v1beta1.SyslogNGClusterOutput
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})
	return res, nil
}

func (r LoggingResourceRepository) SyslogNGFlowsInNamespaceFor(ctx context.Context, namespace string, logging v1beta1.Logging) ([]v1beta1.SyslogNGFlow, error) {
	var list v1beta1.SyslogNGFlowList
	if err := r.Client.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	var res []v1beta1.SyslogNGFlow
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})
	return res, nil
}

func (r LoggingResourceRepository) SyslogNGOutputsInNamespaceFor(ctx context.Context, namespace string, logging v1beta1.Logging) ([]v1beta1.SyslogNGOutput, error) {
	var list v1beta1.SyslogNGOutputList
	if err := r.Client.List(ctx, &list, client.InNamespace(namespace)); err != nil {
		return nil, err
	}

	var res []v1beta1.SyslogNGOutput
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})
	return res, nil
}

func (r LoggingResourceRepository) NodeAgentsFor(ctx context.Context, logging v1beta1.Logging) ([]v1beta1.NodeAgent, error) {
	if os.Getenv("ENABLE_NODEAGENT_CRD") == "" {
		r.Logger.Info("processing NodeAgent CRDs is explicitly disabled (enable: ENABLE_NODEAGENT_CRD=1)")
		return nil, nil
	}

	var list v1beta1.NodeAgentList
	if err := r.Client.List(ctx, &list); err != nil {
		return nil, err
	}

	var res []v1beta1.NodeAgent
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})
	return res, nil
}

func (r LoggingResourceRepository) FluentbitsFor(ctx context.Context, logging v1beta1.Logging) ([]v1beta1.FluentbitAgent, error) {
	var list v1beta1.FluentbitAgentList
	if err := r.Client.List(ctx, &list); err != nil {
		return nil, err
	}

	var res []v1beta1.FluentbitAgent
	for _, i := range list.Items {
		if i.Spec.LoggingRef == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})
	return res, nil
}
func (r LoggingResourceRepository) handleMultipleDetachedFluentdObjects(list *[]v1beta1.Fluentd, logging *v1beta1.Logging) (*v1beta1.Fluentd, error) {
	// TODO add a bit more verbose error handling
	for _, i := range *list {
		if logging.Status.FluentdConfigName != "" {
			if i.Name != logging.Status.FluentdConfigName {
				i.Status.Problems = append(i.Status.Problems, "Logging already has a detached fluentd configuration, remove excess configuration objects")
			}
		}
	}
	multipleFluentdErrors := "multiple fluentd configurations found, couldn't associate it with logging"
	logging.Status.Problems = append(logging.Status.Problems, multipleFluentdErrors)
	return nil, errors.New(multipleFluentdErrors)
}

func (r LoggingResourceRepository) FluentdConfigFor(ctx context.Context, logging *v1beta1.Logging) (*v1beta1.Fluentd, error) {
	var list v1beta1.FluentdList
	if err := r.Client.List(ctx, &list); err != nil {
		return nil, err
	}

	var res []v1beta1.Fluentd
	res = append(res, list.Items...)

	switch len(res) {
	case 0:
		return nil, nil
	case 1:
		// Implicitly associate fluentd configuration object with logging
		detachedFluentd := &res[0]
		err := detachedFluentd.Spec.SetDefaults()
		if err != nil {
			logging.Status.FluentdConfigName = detachedFluentd.Name
			detachedFluentd.Status.Active = utils.BoolPointer(true)
			detachedFluentd.Status.Logging = logging.Name
		}
		return detachedFluentd, err
	default:
		return r.handleMultipleDetachedFluentdObjects(&res, logging)
	}
}

func (r LoggingResourceRepository) LoggingRoutesFor(ctx context.Context, logging v1beta1.Logging) ([]v1beta1.LoggingRoute, error) {
	var list v1beta1.LoggingRouteList
	if err := r.Client.List(ctx, &list); err != nil {
		return nil, err
	}

	var res []v1beta1.LoggingRoute
	for _, i := range list.Items {
		if i.Spec.Source == logging.Spec.LoggingRef {
			res = append(res, i)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return lessByNamespacedName(&res[i], &res[j])
	})
	return res, nil
}

func clusterResourceListOpts(logging v1beta1.Logging) []client.ListOption {
	var opts []client.ListOption
	if !logging.Spec.AllowClusterResourcesFromAllNamespaces {
		opts = append(opts, client.InNamespace(logging.Spec.ControlNamespace))
	}
	return opts
}

func lessByNamespacedName(a, b interface {
	GetNamespace() string
	GetName() string
}) bool {
	if a.GetNamespace() != b.GetNamespace() {
		return a.GetNamespace() < b.GetNamespace()
	}
	return a.GetName() < b.GetName()
}
