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

package common

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	types "github.com/oam-dev/terraform-controller/api/types/crossplane-runtime"

	"github.com/oam-dev/kubevela/apis/core.oam.dev/condition"

	"github.com/oam-dev/kubevela/apis/standard.oam.dev/v1alpha1"
)

// Kube defines the encapsulation in raw Kubernetes resource format
type Kube struct {
	// Template defines the raw Kubernetes resource
	// +kubebuilder:pruning:PreserveUnknownFields
	Template runtime.RawExtension `json:"template"`

	// Parameters defines configurable parameters
	Parameters []KubeParameter `json:"parameters,omitempty"`
}

// ParameterValueType refers to a data type of parameter
type ParameterValueType string

// data types of parameter value
const (
	StringType  ParameterValueType = "string"
	NumberType  ParameterValueType = "number"
	BooleanType ParameterValueType = "boolean"
)

// A KubeParameter defines a configurable parameter of a component.
type KubeParameter struct {
	// Name of this parameter
	Name string `json:"name"`

	// +kubebuilder:validation:Enum:=string;number;boolean
	// ValueType indicates the type of the parameter value, and
	// only supports basic data types: string, number, boolean.
	ValueType ParameterValueType `json:"type"`

	// FieldPaths specifies an array of fields within this workload that will be
	// overwritten by the value of this parameter. 	All fields must be of the
	// same type. Fields are specified as JSON field paths without a leading
	// dot, for example 'spec.replicas'.
	FieldPaths []string `json:"fieldPaths"`

	// +kubebuilder:default:=false
	// Required specifies whether or not a value for this parameter must be
	// supplied when authoring an Application.
	Required *bool `json:"required,omitempty"`

	// Description of this parameter.
	Description *string `json:"description,omitempty"`
}

// CUE defines the encapsulation in CUE format
type CUE struct {
	// Template defines the abstraction template data of the capability, it will replace the old CUE template in extension field.
	// Template is a required field if CUE is defined in Capability Definition.
	Template string `json:"template"`
}

// Schematic defines the encapsulation of this capability(workload/trait/scope),
// the encapsulation can be defined in different ways, e.g. CUE/HCL(terraform)/KUBE(K8s Object)/HELM, etc...
type Schematic struct {
	KUBE *Kube `json:"kube,omitempty"`

	CUE *CUE `json:"cue,omitempty"`

	HELM *Helm `json:"helm,omitempty"`

	Terraform *Terraform `json:"terraform,omitempty"`
}

// A Helm represents resources used by a Helm module
type Helm struct {
	// Release records a Helm release used by a Helm module workload.
	// +kubebuilder:pruning:PreserveUnknownFields
	Release runtime.RawExtension `json:"release"`

	// HelmRelease records a Helm repository used by a Helm module workload.
	// +kubebuilder:pruning:PreserveUnknownFields
	Repository runtime.RawExtension `json:"repository"`
}

// Terraform is the struct to describe cloud resources managed by Hashicorp Terraform
type Terraform struct {
	// Configuration is Terraform Configuration
	Configuration string `json:"configuration"`

	// Type specifies which Terraform configuration it is, HCL or JSON syntax
	// +kubebuilder:default:=hcl
	// +kubebuilder:validation:Enum:=hcl;json;remote
	Type string `json:"type,omitempty"`

	// ProviderReference specifies the reference to Provider
	ProviderReference *types.Reference `json:"providerRef,omitempty"`
}

// A WorkloadTypeDescriptor refer to a Workload Type
type WorkloadTypeDescriptor struct {
	// Type ref to a WorkloadDefinition via name
	Type string `json:"type,omitempty"`
	// Definition mutually exclusive to workload.type, a embedded WorkloadDefinition
	Definition WorkloadGVK `json:"definition,omitempty"`
}

// WorkloadGVK refer to a Workload Type
type WorkloadGVK struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
}

// A DefinitionReference refers to a CustomResourceDefinition by name.
type DefinitionReference struct {
	// Name of the referenced CustomResourceDefinition.
	Name string `json:"name"`

	// Version indicate which version should be used if CRD has multiple versions
	// by default it will use the first one if not specified
	Version string `json:"version,omitempty"`
}

// A ChildResourceKind defines a child Kubernetes resource kind with a selector
type ChildResourceKind struct {
	// APIVersion of the child resource
	APIVersion string `json:"apiVersion"`

	// Kind of the child resource
	Kind string `json:"kind"`

	// Selector to select the child resources that the workload wants to expose to traits
	Selector map[string]string `json:"selector,omitempty"`
}

// Status defines the loop back status of the abstraction by using CUE template
type Status struct {
	// CustomStatus defines the custom status message that could display to user
	// +optional
	CustomStatus string `json:"customStatus,omitempty"`
	// HealthPolicy defines the health check policy for the abstraction
	// +optional
	HealthPolicy string `json:"healthPolicy,omitempty"`
}

// ApplicationPhase is a label for the condition of a application at the current time
type ApplicationPhase string

const (
	// ApplicationRollingOut means the app is in the middle of rolling out
	ApplicationRollingOut ApplicationPhase = "rollingOut"
	// ApplicationStarting means the app is preparing for reconcile
	ApplicationStarting ApplicationPhase = "starting"
	// ApplicationRendering means the app is rendering
	ApplicationRendering ApplicationPhase = "rendering"
	// ApplicationPolicyGenerating means the app is generating policies
	ApplicationPolicyGenerating ApplicationPhase = "generatingPolicy"
	// ApplicationRunningWorkflow means the app is running workflow
	ApplicationRunningWorkflow ApplicationPhase = "runningWorkflow"
	// ApplicationWorkflowSuspending means the app's workflow is suspending
	ApplicationWorkflowSuspending ApplicationPhase = "workflowSuspending"
	// ApplicationWorkflowTerminated means the app's workflow is terminated
	ApplicationWorkflowTerminated ApplicationPhase = "workflowTerminated"
	// ApplicationWorkflowFinished means the app's workflow is finished
	ApplicationWorkflowFinished ApplicationPhase = "workflowFinished"
	// ApplicationRunning means the app finished rendering and applied result to the cluster
	ApplicationRunning ApplicationPhase = "running"
	// ApplicationUnhealthy means the app finished rendering and applied result to the cluster, but still unhealthy
	ApplicationUnhealthy ApplicationPhase = "unhealthy"
)

// WorkflowState is a string that mark the workflow state
type WorkflowState string

const (
	// WorkflowStateTerminated means workflow is terminated manually, and it won't be started unless the spec changed.
	WorkflowStateTerminated WorkflowState = "terminated"
	// WorkflowStateSuspended means workflow is suspended manually, and it can be resumed.
	WorkflowStateSuspended WorkflowState = "suspended"
	// WorkflowStateFinished means workflow is running successfully, all steps finished.
	WorkflowStateFinished WorkflowState = "finished"
	// WorkflowStateExecuting means workflow is still running or waiting some steps.
	WorkflowStateExecuting WorkflowState = "executing"
)

// ApplicationComponentStatus record the health status of App component
type ApplicationComponentStatus struct {
	Name string `json:"name"`
	Env  string `json:"env,omitempty"`
	// WorkloadDefinition is the definition of a WorkloadDefinition, such as deployments/apps.v1
	WorkloadDefinition WorkloadGVK              `json:"workloadDefinition,omitempty"`
	Healthy            bool                     `json:"healthy"`
	Message            string                   `json:"message,omitempty"`
	Traits             []ApplicationTraitStatus `json:"traits,omitempty"`
	Scopes             []corev1.ObjectReference `json:"scopes,omitempty"`
}

// ApplicationTraitStatus records the trait health status
type ApplicationTraitStatus struct {
	Type    string `json:"type"`
	Healthy bool   `json:"healthy"`
	Message string `json:"message,omitempty"`
}

// Revision has name and revision number
type Revision struct {
	Name     string `json:"name"`
	Revision int64  `json:"revision"`

	// RevisionHash record the hash value of the spec of ApplicationRevision object.
	RevisionHash string `json:"revisionHash,omitempty"`
}

// RawComponent record raw component
type RawComponent struct {
	// +kubebuilder:validation:EmbeddedResource
	// +kubebuilder:pruning:PreserveUnknownFields
	Raw runtime.RawExtension `json:"raw"`
}

// WorkflowStepStatus record the status of a workflow step
type WorkflowStepStatus struct {
	ID    string            `json:"id"`
	Name  string            `json:"name,omitempty"`
	Type  string            `json:"type,omitempty"`
	Phase WorkflowStepPhase `json:"phase,omitempty"`
	// A human readable message indicating details about why the workflowStep is in this state.
	Message string `json:"message,omitempty"`
	// A brief CamelCase message indicating details about why the workflowStep is in this state.
	Reason   string          `json:"reason,omitempty"`
	SubSteps *SubStepsStatus `json:"subSteps,omitempty"`
}

// WorkflowSubStepStatus record the status of a workflow step
type WorkflowSubStepStatus struct {
	ID    string            `json:"id"`
	Name  string            `json:"name,omitempty"`
	Type  string            `json:"type,omitempty"`
	Phase WorkflowStepPhase `json:"phase,omitempty"`
	// A human readable message indicating details about why the workflowStep is in this state.
	Message string `json:"message,omitempty"`
	// A brief CamelCase message indicating details about why the workflowStep is in this state.
	Reason string `json:"reason,omitempty"`
}

// AppStatus defines the observed state of Application
type AppStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	condition.ConditionedStatus `json:",inline"`

	// The generation observed by the application controller.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	Rollout *AppRolloutStatus `json:"rollout,omitempty"`

	Phase ApplicationPhase `json:"status,omitempty"`

	// Components record the related Components created by Application Controller
	Components []corev1.ObjectReference `json:"components,omitempty"`

	// Services record the status of the application services
	Services []ApplicationComponentStatus `json:"services,omitempty"`

	// ResourceTracker record the status of the ResourceTracker
	ResourceTracker *corev1.ObjectReference `json:"resourceTracker,omitempty"`

	// Workflow record the status of workflow
	Workflow *WorkflowStatus `json:"workflow,omitempty"`

	// LatestRevision of the application configuration it generates
	// +optional
	LatestRevision *Revision `json:"latestRevision,omitempty"`

	// AppliedResources record the resources that the  workflow step apply.
	AppliedResources []ClusterObjectReference `json:"appliedResources,omitempty"`
}

// WorkflowStatus record the status of workflow
type WorkflowStatus struct {
	AppRevision string       `json:"appRevision,omitempty"`
	Mode        WorkflowMode `json:"mode"`

	Suspend    bool `json:"suspend"`
	Terminated bool `json:"terminated"`

	ContextBackend *corev1.ObjectReference `json:"contextBackend,omitempty"`
	Steps          []WorkflowStepStatus    `json:"steps,omitempty"`
}

// SubStepsStatus record the status of workflow steps.
type SubStepsStatus struct {
	StepIndex int                     `json:"stepIndex,omitempty"`
	Mode      WorkflowMode            `json:"mode,omitempty"`
	Steps     []WorkflowSubStepStatus `json:"steps,omitempty"`
}

// WorkflowStepPhase describes the phase of a workflow step.
type WorkflowStepPhase string

const (
	// WorkflowStepPhaseSucceeded will make the controller run the next step.
	WorkflowStepPhaseSucceeded WorkflowStepPhase = "succeeded"
	// WorkflowStepPhaseFailed will make the controller stop the workflow and report error in `message`.
	WorkflowStepPhaseFailed WorkflowStepPhase = "failed"
	// WorkflowStepPhaseStopped will make the controller stop the workflow.
	WorkflowStepPhaseStopped WorkflowStepPhase = "stopped"
	// WorkflowStepPhaseRunning will make the controller continue the workflow.
	WorkflowStepPhaseRunning WorkflowStepPhase = "running"
)

// DefinitionType describes the type of DefinitionRevision.
// +kubebuilder:validation:Enum=Component;Trait;Policy;WorkflowStep
type DefinitionType string

const (
	// ComponentType represents DefinitionRevision refer to type ComponentDefinition
	ComponentType DefinitionType = "Component"

	// TraitType represents DefinitionRevision refer to type TraitDefinition
	TraitType DefinitionType = "Trait"

	// PolicyType represents DefinitionRevision refer to type PolicyDefinition
	PolicyType DefinitionType = "Policy"

	// WorkflowStepType represents DefinitionRevision refer to type WorkflowStepDefinition
	WorkflowStepType DefinitionType = "WorkflowStep"
)

// WorkflowMode describes the mode of workflow
type WorkflowMode string

const (
	// WorkflowModeDAG describes the DAG mode of workflow
	WorkflowModeDAG WorkflowMode = "DAG"
	// WorkflowModeStep describes the step by step mode of workflow
	WorkflowModeStep WorkflowMode = "StepByStep"
)

// AppRolloutStatus defines the observed state of AppRollout
type AppRolloutStatus struct {
	v1alpha1.RolloutStatus `json:",inline"`

	// LastUpgradedTargetAppRevision contains the name of the app that we upgraded to
	// We will restart the rollout if this is not the same as the spec
	LastUpgradedTargetAppRevision string `json:"lastTargetAppRevision"`

	// LastSourceAppRevision contains the name of the app that we need to upgrade from.
	// We will restart the rollout if this is not the same as the spec
	LastSourceAppRevision string `json:"LastSourceAppRevision,omitempty"`
}

// ApplicationTrait defines the trait of application
type ApplicationTrait struct {
	Type string `json:"type"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Properties runtime.RawExtension `json:"properties,omitempty"`
}

// ApplicationComponent describe the component of application
type ApplicationComponent struct {
	Name string `json:"name"`
	Type string `json:"type"`
	// ExternalRevision specified the component revisionName
	ExternalRevision string `json:"externalRevision,omitempty"`
	// +kubebuilder:pruning:PreserveUnknownFields
	Properties runtime.RawExtension `json:"properties,omitempty"`

	DependsOn []string    `json:"dependsOn,omitempty"`
	Inputs    StepInputs  `json:"inputs,omitempty"`
	Outputs   StepOutputs `json:"outputs,omitempty"`

	// Traits define the trait of one component, the type must be array to keep the order.
	Traits []ApplicationTrait `json:"traits,omitempty"`

	// +kubebuilder:pruning:PreserveUnknownFields
	// scopes in ApplicationComponent defines the component-level scopes
	// the format is <scope-type:scope-instance-name> pairs, the key represents type of `ScopeDefinition` while the value represent the name of scope instance.
	Scopes map[string]string `json:"scopes,omitempty"`
}

// StepOutputs defines output variable of WorkflowStep
type StepOutputs []outputItem

// StepInputs defines variable input of WorkflowStep
type StepInputs []inputItem

type inputItem struct {
	ParameterKey string `json:"parameterKey"`
	From         string `json:"from"`
}

type outputItem struct {
	ValueFrom string `json:"valueFrom"`
	Name      string `json:"name"`
}

// ClusterSelector defines the rules to select a Cluster resource.
// Either name or labels is needed.
type ClusterSelector struct {
	// Name is the name of the cluster.
	Name string `json:"name,omitempty"`

	// Labels defines the label selector to select the cluster.
	Labels map[string]string `json:"labels,omitempty"`
}

// Distribution defines the replica distribution of an AppRevision to a cluster.
type Distribution struct {
	// Replicas is the replica number.
	Replicas int `json:"replicas,omitempty"`
}

// ClusterPlacement defines the cluster placement rules for an app revision.
type ClusterPlacement struct {
	// ClusterSelector selects the cluster to  deploy apps to.
	// If not specified, it indicates the host cluster per se.
	ClusterSelector *ClusterSelector `json:"clusterSelector,omitempty"`

	// Distribution defines the replica distribution of an AppRevision to a cluster.
	Distribution Distribution `json:"distribution,omitempty"`
}

// ResourceCreatorRole defines the resource creator.
type ResourceCreatorRole string

const (
	// PolicyResourceCreator create the policy resource.
	PolicyResourceCreator ResourceCreatorRole = "policy"
	// WorkflowResourceCreator create the resource in workflow.
	WorkflowResourceCreator ResourceCreatorRole = "workflow"
)

// ClusterObjectReference defines the object reference with cluster.
type ClusterObjectReference struct {
	Cluster                string              `json:"cluster,omitempty"`
	Creator                ResourceCreatorRole `json:"creator,omitempty"`
	corev1.ObjectReference `json:",inline"`
}
