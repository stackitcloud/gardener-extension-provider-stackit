// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package helper

import (
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/controller"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/apis/stackit/v1alpha1"
)

var (
	// Scheme is a scheme with the types relevant for OpenStack actuators.
	Scheme *runtime.Scheme

	decoder runtime.Decoder

	// lenientDecoder is a decoder that does not use strict mode.
	lenientDecoder runtime.Decoder
)

func init() {
	Scheme = runtime.NewScheme()
	utilruntime.Must(stackitv1alpha1.AddToScheme(Scheme))

	decoder = serializer.NewCodecFactory(Scheme, serializer.EnableStrict).UniversalDecoder()
	lenientDecoder = serializer.NewCodecFactory(Scheme).UniversalDecoder()
}

// InfrastructureConfigFromInfrastructure extracts the InfrastructureConfig from the
// ProviderConfig section of the given Infrastructure.
func InfrastructureConfigFromInfrastructure(infra *extensionsv1alpha1.Infrastructure) (*stackitv1alpha1.InfrastructureConfig, error) {
	return InfrastructureConfigFromRawExtension(infra.Spec.ProviderConfig)
}

// InfrastructureConfigFromRawExtension extracts the InfrastructureConfig from the ProviderConfig.
func InfrastructureConfigFromRawExtension(raw *runtime.RawExtension) (*stackitv1alpha1.InfrastructureConfig, error) {
	config := &stackitv1alpha1.InfrastructureConfig{}
	setGVK(config)

	if err := decode(raw, config); err != nil {
		return nil, err
	}
	return config, nil
}

// InfrastructureStatusFromRaw extracts the InfrastructureStatus from the
// ProviderStatus section of the given Infrastructure.
func InfrastructureStatusFromRaw(raw *runtime.RawExtension) (*stackitv1alpha1.InfrastructureStatus, error) {
	status := &stackitv1alpha1.InfrastructureStatus{}
	setGVK(status)

	if err := decodeWith(lenientDecoder, raw, nil, status); err != nil {
		return nil, err
	}
	return status, nil
}

// CloudProfileConfigFromCluster decodes the provider specific cloud profile configuration for a cluster
func CloudProfileConfigFromCluster(cluster *controller.Cluster) (*stackitv1alpha1.CloudProfileConfig, error) {
	cloudProfileConfig := &stackitv1alpha1.CloudProfileConfig{}
	setGVK(cloudProfileConfig)

	if cluster == nil || cluster.CloudProfile == nil {
		return cloudProfileConfig, nil
	}

	cloudProfileSpecifier := fmt.Sprintf("cloudProfile '%q'", k8sclient.ObjectKeyFromObject(cluster.CloudProfile))
	if cluster.Shoot != nil && cluster.Shoot.Spec.CloudProfile != nil {
		cloudProfileSpecifier = fmt.Sprintf("%s '%s/%s'", cluster.Shoot.Spec.CloudProfile.Kind, cluster.Shoot.Namespace, cluster.Shoot.Spec.CloudProfile.Name)
	}

	if err := decode(cluster.CloudProfile.Spec.ProviderConfig, cloudProfileConfig); err != nil {
		return nil, fmt.Errorf("could not decode providerConfig of %s: %w", cloudProfileSpecifier, err)
	}
	return cloudProfileConfig, nil
}

func CloudProfileConfigFromRawExtension(raw *runtime.RawExtension) (*stackitv1alpha1.CloudProfileConfig, error) {
	cpConfig := &stackitv1alpha1.CloudProfileConfig{}
	setGVK(cpConfig)

	if err := decode(raw, cpConfig); err != nil {
		return nil, err
	}
	return cpConfig, nil
}

// WorkerConfigFromRawExtension extracts the provider specific configuration for a worker pool.
func WorkerConfigFromRawExtension(raw *runtime.RawExtension) (*stackitv1alpha1.WorkerConfig, error) {
	workerConfig := &stackitv1alpha1.WorkerConfig{}
	setGVK(workerConfig)

	if raw != nil {
		marshaled, err := raw.MarshalJSON()
		if err != nil {
			return nil, err
		}
		marshaledExt := &runtime.RawExtension{
			Raw: marshaled,
		}
		if err := decode(marshaledExt, workerConfig); err != nil {
			return nil, err
		}
	}
	return workerConfig, nil
}

// ControlPlaneConfigFromCluster retrieves the ControlPlaneConfig from the Cluster. Returns nil if decoding fails
func ControlPlaneConfigFromCluster(cluster *controller.Cluster) (*stackitv1alpha1.ControlPlaneConfig, error) {
	cpConfig := &stackitv1alpha1.ControlPlaneConfig{}
	setGVK(cpConfig)

	if cluster == nil || cluster.Shoot == nil {
		return cpConfig, nil
	}
	if err := decode(cluster.Shoot.Spec.Provider.ControlPlaneConfig, cpConfig); err != nil {
		return nil, err
	}

	return cpConfig, nil
}

type objectWithGVK interface {
	runtime.Object
	SetGroupVersionKind(gvk schema.GroupVersionKind)
}

// setGVK sets the type meta based on the scheme. We do this to ensure that we always have a valid type meta (apiVersion
// + kind) when returning the object.
func setGVK(obj objectWithGVK) {
	gkv, err := apiutil.GVKForObject(obj, Scheme)
	if err != nil {
		panic(fmt.Errorf("could not get kinds from schema: %w", err))
	}
	obj.SetGroupVersionKind(gkv)
}

func decode(raw *runtime.RawExtension, into objectWithGVK) error {
	return decodeWith(decoder, raw, nil, into)
}

// decodeWith decodes the given raw extension into the given object using the given decoder. After decoding, it ensures
// that the type meta is set correctly.
func decodeWith(dec runtime.Decoder, raw *runtime.RawExtension, defaults *schema.GroupVersionKind, into objectWithGVK) error {
	var data []byte
	if raw != nil {
		data = raw.Raw
	}
	_, gkv, err := dec.Decode(data, defaults, into)
	if err != nil {
		return err
	}
	if gkv != nil {
		into.SetGroupVersionKind(*gkv)
	}
	return nil
}
