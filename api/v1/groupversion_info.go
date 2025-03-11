/*
Copyright 2023.

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

// Package v1 contains API Schema definitions for the adx-mon v1 API group
// +kubebuilder:object:generate=true
// +groupName=adx-mon.azure.com
package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "adx-mon.azure.com", Version: "v1"}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// ControllerOwnerKey is the annotation key used to identify the owner of a controller.
	// Enables distributed ownership of resources, allowing multiple controllers to
	// manage the same resource type.
	ControllerOwnerKey = "controller.adx-mon.azure.com/owner"

	// LastUpdatedKey is the annotation key used to track the last update time of a resource
	// in an effort to detect staleness or a controller owner that no longer exists or has
	// failed to make forward progress.
	LastUpdatedKey = "controller.adx-mon.azure.com/last-updated"

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)
