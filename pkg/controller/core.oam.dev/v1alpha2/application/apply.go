/*
Copyright 2021 The KubeVela Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package application

import (
	"context"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	terraformtypes "github.com/oam-dev/terraform-controller/api/types"
	terraformapi "github.com/oam-dev/terraform-controller/api/v1beta1"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/common"
	"github.com/oam-dev/kubevela/apis/core.oam.dev/v1beta1"
	"github.com/oam-dev/kubevela/apis/types"
	"github.com/oam-dev/kubevela/pkg/appfile"
	"github.com/oam-dev/kubevela/pkg/controller/core.oam.dev/v1alpha2/application/dispatch"
	"github.com/oam-dev/kubevela/pkg/controller/core.oam.dev/v1alpha2/applicationrollout"
	"github.com/oam-dev/kubevela/pkg/controller/utils"
	"github.com/oam-dev/kubevela/pkg/cue/process"
	"github.com/oam-dev/kubevela/pkg/oam"
	oamutil "github.com/oam-dev/kubevela/pkg/oam/util"
)

// AppHandler handles application reconcile
type AppHandler struct {
	r              *Reconciler
	app            *v1beta1.Application
	currentAppRev  *v1beta1.ApplicationRevision
	latestAppRev   *v1beta1.ApplicationRevision
	latestTracker  *v1beta1.ResourceTracker
	dispatcher     *dispatch.AppManifestsDispatcher
	isNewRevision  bool
	currentRevHash string

	services         []common.ApplicationComponentStatus
	appliedResources []common.ClusterObjectReference
	parser           *appfile.Parser
}

// Dispatch apply manifests into k8s.
func (h *AppHandler) Dispatch(ctx context.Context, cluster string, owner common.ResourceCreatorRole, manifests ...*unstructured.Unstructured) error {
	h.initDispatcher()
	_, err := h.dispatcher.Dispatch(ctx, manifests)
	if err == nil {
		for _, mf := range manifests {
			if mf == nil {
				continue
			}
			ref := common.ClusterObjectReference{
				Cluster: cluster,
				Creator: owner,
				ObjectReference: corev1.ObjectReference{
					Name:       mf.GetName(),
					Namespace:  mf.GetNamespace(),
					Kind:       mf.GetKind(),
					APIVersion: mf.GetAPIVersion(),
				},
			}
			h.addAppliedResource(ref)
		}
	}
	return err
}

// addAppliedResource recorde applied resource.
// reconcile run at single threaded. So there is no need to consider to use locker.
func (h *AppHandler) addAppliedResource(refs ...common.ClusterObjectReference) {
	for _, ref := range refs {
		found := false
		for _, current := range h.appliedResources {
			if isSameObjReference(current, ref) {
				found = true
				break
			}
		}
		if !found {
			h.appliedResources = append(h.appliedResources, ref)
		}
	}
}

func isSameObjReference(ref1, ref2 common.ClusterObjectReference) bool {
	return ref1.Cluster == ref2.Cluster &&
		ref1.Namespace == ref2.Namespace &&
		ref1.APIVersion == ref2.APIVersion &&
		ref1.Kind == ref2.Kind &&
		ref1.Name == ref2.Name
}

// addServiceStatus recorde the whole component status.
// reconcile run at single threaded. So there is no need to consider to use locker.
func (h *AppHandler) addServiceStatus(cover bool, svcs ...common.ApplicationComponentStatus) {
	for _, svc := range svcs {
		found := false
		for i := range h.services {
			current := h.services[i]
			if current.Name == svc.Name {
				if cover {
					h.services[i] = svc
				}
				found = true
				break
			}
		}
		if !found {
			h.services = append(h.services, svc)
		}
	}
}

// DispatchAndGC apply manifests and do GC.
func (h *AppHandler) DispatchAndGC(ctx context.Context, manifests ...*unstructured.Unstructured) (*corev1.ObjectReference, error) {
	h.initDispatcher()
	tracker, err := h.dispatcher.EndAndGC(h.latestTracker).Dispatch(ctx, manifests)
	if err != nil {
		return nil, errors.WithMessage(err, "cannot dispatch application manifests")
	}
	return &corev1.ObjectReference{
		APIVersion: tracker.APIVersion,
		Kind:       tracker.Kind,
		Name:       tracker.Name,
		UID:        tracker.UID,
	}, nil
}

func (h *AppHandler) initDispatcher() {
	if h.latestTracker == nil {
		if h.app.Status.ResourceTracker != nil {
			h.latestTracker = &v1beta1.ResourceTracker{}
			h.latestTracker.Name = h.app.Status.ResourceTracker.Name
		} else if h.app.Status.LatestRevision != nil {
			h.latestTracker = &v1beta1.ResourceTracker{}
			h.latestTracker.SetName(dispatch.ConstructResourceTrackerName(h.app.Status.LatestRevision.Name, h.app.Namespace))
		}
	}
	if h.dispatcher == nil {
		// only do GC when ALL resources are dispatched successfully
		// so skip GC while dispatching addon resources
		h.dispatcher = dispatch.NewAppManifestsDispatcher(h.r.Client, h.currentAppRev).StartAndSkipGC(h.latestTracker)
	}
}

// ProduceArtifacts will produce Application artifacts that will be saved in configMap.
func (h *AppHandler) ProduceArtifacts(ctx context.Context, comps []*types.ComponentManifest, policies []*unstructured.Unstructured) error {
	return h.createResourcesConfigMap(ctx, h.currentAppRev, comps, policies)
}

func (h *AppHandler) collectHealthStatus(wl *appfile.Workload, appRev *v1beta1.ApplicationRevision) (*common.ApplicationComponentStatus, bool, error) {

	var (
		status = common.ApplicationComponentStatus{
			Name:               wl.Name,
			WorkloadDefinition: wl.FullTemplate.Reference.Definition,
			Healthy:            true,
		}
		appName  = appRev.Spec.Application.Name
		isHealth = true
	)

	if wl.CapabilityCategory == types.TerraformCategory {
		return nil, true, nil
	}

	if ok, err := wl.EvalHealth(wl.Ctx, h.r.Client, h.app.Namespace); !ok || err != nil {
		isHealth = false
		status.Healthy = false
	}
	var traitStatusList []common.ApplicationTraitStatus

	var err error
	status.Message, err = wl.EvalStatus(wl.Ctx, h.r.Client, h.app.Namespace)
	if err != nil {
		return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, evaluate workload status message error", appName, wl.Name)
	}

	for _, tr := range wl.Traits {
		var traitStatus = common.ApplicationTraitStatus{
			Type:    tr.Name,
			Healthy: true,
		}
		if ok, err := tr.EvalHealth(wl.Ctx, h.r.Client, h.app.Namespace); !ok || err != nil {
			isHealth = false
			traitStatus.Healthy = false
		}
		traitStatus.Message, err = tr.EvalStatus(wl.Ctx, h.r.Client, h.app.Namespace)
		if err != nil {
			return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, trait=%s, evaluate status message error", appName, wl.Name, tr.Name)
		}
		traitStatusList = append(traitStatusList, traitStatus)
	}

	status.Traits = traitStatusList
	status.Scopes = generateScopeReference(wl.Scopes)
	h.addServiceStatus(true, status)
	return &status, isHealth, nil
}

func (h *AppHandler) aggregateHealthStatus(appFile *appfile.Appfile) ([]common.ApplicationComponentStatus, bool, error) {
	var appStatus []common.ApplicationComponentStatus
	var healthy = true
	for _, wl := range appFile.Workloads {
		var status = common.ApplicationComponentStatus{
			Name:               wl.Name,
			WorkloadDefinition: wl.FullTemplate.Reference.Definition,
			Healthy:            true,
		}

		var pCtx process.Context

		switch wl.CapabilityCategory {
		case types.TerraformCategory:
			pCtx = appfile.NewBasicContext(wl, appFile.Name, appFile.AppRevisionName, appFile.Namespace)
			ctx := context.Background()
			var configuration terraformapi.Configuration
			if err := h.r.Client.Get(ctx, client.ObjectKey{Name: wl.Name, Namespace: h.app.Namespace}, &configuration); err != nil {
				return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, check health error", appFile.Name, wl.Name)
			}
			if configuration.Status.Apply.State != terraformtypes.Available {
				healthy = false
				status.Healthy = false
			} else {
				status.Healthy = true
			}
			status.Message = configuration.Status.Apply.Message
		default:
			pCtx = process.NewContext(h.app.Namespace, wl.Name, appFile.Name, appFile.AppRevisionName)
			if !h.isNewRevision && wl.CapabilityCategory != types.CUECategory {
				templateStr, err := appfile.GenerateCUETemplate(wl)
				if err != nil {
					return nil, false, err
				}
				wl.FullTemplate.TemplateStr = templateStr
			}

			if err := wl.EvalContext(pCtx); err != nil {
				return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, evaluate context error", appFile.Name, wl.Name)
			}
			workloadHealth, err := wl.EvalHealth(pCtx, h.r.Client, h.app.Namespace)
			if err != nil {
				return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, check health error", appFile.Name, wl.Name)
			}
			if !workloadHealth {
				// TODO(wonderflow): we should add a custom way to let the template say why it's unhealthy, only a bool flag is not enough
				status.Healthy = false
				healthy = false
			}

			status.Message, err = wl.EvalStatus(pCtx, h.r.Client, h.app.Namespace)
			if err != nil {
				return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, evaluate workload status message error", appFile.Name, wl.Name)
			}
		}

		var traitStatusList []common.ApplicationTraitStatus
		for _, tr := range wl.Traits {
			if err := tr.EvalContext(pCtx); err != nil {
				return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, trait=%s, evaluate context error", appFile.Name, wl.Name, tr.Name)
			}

			var traitStatus = common.ApplicationTraitStatus{
				Type:    tr.Name,
				Healthy: true,
			}
			traitHealth, err := tr.EvalHealth(pCtx, h.r.Client, h.app.Namespace)
			if err != nil {
				return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, trait=%s, check health error", appFile.Name, wl.Name, tr.Name)
			}
			if !traitHealth {
				// TODO(wonderflow): we should add a custom way to let the template say why it's unhealthy, only a bool flag is not enough
				traitStatus.Healthy = false
				healthy = false
			}
			traitStatus.Message, err = tr.EvalStatus(pCtx, h.r.Client, h.app.Namespace)
			if err != nil {
				return nil, false, errors.WithMessagef(err, "app=%s, comp=%s, trait=%s, evaluate status message error", appFile.Name, wl.Name, tr.Name)
			}
			traitStatusList = append(traitStatusList, traitStatus)
		}

		status.Traits = traitStatusList
		status.Scopes = generateScopeReference(wl.Scopes)
		appStatus = append(appStatus, status)
	}
	return appStatus, healthy, nil
}

func (h *AppHandler) handleCheckManageWorkloadTrait(traitDefs map[string]v1beta1.TraitDefinition, comps []*types.ComponentManifest) {
	manageWorkloadTrait := map[string]bool{}
	for traitName, definition := range traitDefs {
		if definition.Spec.ManageWorkload {
			manageWorkloadTrait[traitName] = true
		}
	}
	if len(manageWorkloadTrait) == 0 {
		return
	}
	for _, comp := range comps {
		for _, trait := range comp.Traits {
			traitType := trait.GetLabels()[oam.TraitTypeLabel]
			if manageWorkloadTrait[traitType] {
				trait.SetLabels(oamutil.MergeMapOverrideWithDst(trait.GetLabels(), map[string]string{oam.LabelManageWorkloadTrait: "true"}))
			}
		}
	}
}

func generateScopeReference(scopes []appfile.Scope) []corev1.ObjectReference {
	var references []corev1.ObjectReference
	for _, scope := range scopes {
		references = append(references, corev1.ObjectReference{
			APIVersion: metav1.GroupVersion{
				Group:   scope.GVK.Group,
				Version: scope.GVK.Version,
			}.String(),
			Kind: scope.GVK.Kind,
			Name: scope.Name,
		})
	}
	return references
}

type garbageCollectFunc func(ctx context.Context, h *AppHandler) error

// execute garbage collection functions, including:
// - clean up legacy app revisions
// - clean up legacy component revisions
func garbageCollection(ctx context.Context, h *AppHandler) error {
	collectFuncs := []garbageCollectFunc{
		garbageCollectFunc(cleanUpApplicationRevision),
		garbageCollectFunc(cleanUpComponentRevision),
	}
	for _, collectFunc := range collectFuncs {
		if err := collectFunc(ctx, h); err != nil {
			return err
		}
	}
	return nil
}

func (h *AppHandler) handleRollout(ctx context.Context) (reconcile.Result, error) {
	var comps []string
	for _, component := range h.app.Spec.Components {
		comps = append(comps, component.Name)
		// TODO rollout only support one component now, and we only rollout the first component in Application
		break
	}

	// targetRevision should always points to LatestRevison
	targetRevision := h.app.Status.LatestRevision.Name
	var srcRevision string
	target, _ := oamutil.ExtractRevisionNum(targetRevision, "-")
	// if target == 1 this is a initial scale operation, sourceRevision should be empty
	// otherwise source revision always is targetRevision - 1
	if target > 1 {
		srcRevision = utils.ConstructRevisionName(h.app.Name, int64(target-1))
	}

	appRollout := v1beta1.AppRollout{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.SchemeGroupVersion.String(),
			Kind:       v1beta1.ApplicationKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      h.app.Name,
			Namespace: h.app.Namespace,
			UID:       h.app.UID,
		},
		Spec: v1beta1.AppRolloutSpec{
			SourceAppRevisionName: srcRevision,
			TargetAppRevisionName: targetRevision,
			ComponentList:         comps,
			RolloutPlan:           *h.app.Spec.RolloutPlan,
		},
	}
	if h.app.Status.Rollout != nil {
		appRollout.Status = *h.app.Status.Rollout
	} else {
		appRollout.Status = common.AppRolloutStatus{}
	}
	// construct a fake rollout object and call rollout.DoReconcile
	r := applicationrollout.NewReconciler(h.r.Client, h.r.dm, h.r.pd, h.r.Recorder, h.r.Scheme, h.r.concurrentReconciles)
	res, err := r.DoReconcile(ctx, &appRollout)
	if err != nil {
		return reconcile.Result{}, err
	}

	// write back rollout status to application
	h.app.Status.Rollout = &appRollout.Status
	return res, nil
}
