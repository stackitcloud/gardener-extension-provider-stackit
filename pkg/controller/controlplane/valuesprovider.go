// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package controlplane

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	calicov1alpha1 "github.com/gardener/gardener-extension-networking-calico/pkg/apis/calico/v1alpha1"
	"github.com/gardener/gardener-extension-networking-calico/pkg/calico"
	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/controlplane/genericactuator"
	extensionssecretmanager "github.com/gardener/gardener/extensions/pkg/util/secret/manager"
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	gardenerutils "github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/chart"
	gutil "github.com/gardener/gardener/pkg/utils/gardener"
	kutil "github.com/gardener/gardener/pkg/utils/kubernetes"
	secretutils "github.com/gardener/gardener/pkg/utils/secrets"
	secretsmanager "github.com/gardener/gardener/pkg/utils/secrets/manager"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	vpaautoscalingv1 "k8s.io/autoscaler/vertical-pod-autoscaler/pkg/apis/autoscaling.k8s.io/v1"
	"k8s.io/utils/ptr"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/charts"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/imagevector"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/utils"
)

const (
	caNameControlPlane               = "ca-" + openstack.Name + "-controlplane"
	cloudControllerManagerServerName = openstack.CloudControllerManagerName + "-server"

	CSIStackitPrefix = "stackit-blockstorage"

	// LoadBalancerEmergencyAccessSecretName defines the name of the secret which, when deployed,
	// will reconfigure the CCM and bypass the LoadBalancer API Gateway.
	LoadBalancerEmergencyAccessSecretName  = "lb-api-emergency-access"
	LoadBalancerEmergencyAccessAPIURLKey   = "lbApiUrl"
	LoadBalancerEmergencyAccessAPITokenKey = "lbApiToken"

	STACKITCCMServiceLoadbalancerController = "service-lb-controller"
	// TODO: migrate to utils.BuildLabelKey
	STACKITLBClusterLabelKey = "cluster.stackit.cloud"
)

var constraintK8sEquals129 *semver.Constraints

func init() {
	var err error
	constraintK8sEquals129, err = semver.NewConstraint("= 1.29-0")
	utilruntime.Must(err)
}

func secretConfigsFunc(namespace string) []extensionssecretmanager.SecretConfigWithOptions {
	return []extensionssecretmanager.SecretConfigWithOptions{
		{
			Config: &secretutils.CertificateSecretConfig{
				Name:       caNameControlPlane,
				CommonName: caNameControlPlane,
				CertType:   secretutils.CACert,
			},
			Options: []secretsmanager.GenerateOption{secretsmanager.Persist()},
		},
		{
			Config: &secretutils.CertificateSecretConfig{
				Name:                        cloudControllerManagerServerName,
				CommonName:                  openstack.CloudControllerManagerName,
				DNSNames:                    kutil.DNSNamesForService(openstack.CloudControllerManagerName, namespace),
				CertType:                    secretutils.ServerCert,
				SkipPublishingCACertificate: true,
			},
			Options: []secretsmanager.GenerateOption{secretsmanager.SignedByCA(caNameControlPlane)},
		},
	}
}

func shootAccessSecretsFunc(namespace string) []*gutil.AccessSecret {
	return []*gutil.AccessSecret{
		gutil.NewShootAccessSecret(openstack.CloudControllerManagerName, namespace),
		gutil.NewShootAccessSecret(openstack.CSIProvisionerName, namespace),
		gutil.NewShootAccessSecret(openstack.CSIAttacherName, namespace),
		gutil.NewShootAccessSecret(openstack.CSISnapshotterName, namespace),
		gutil.NewShootAccessSecret(openstack.CSIResizerName, namespace),
		gutil.NewShootAccessSecret(openstack.CSISnapshotControllerName, namespace),
	}
}

func makeUnstructured(gvk schema.GroupVersionKind) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(gvk)
	return obj
}

var (
	configChart = &chart.Chart{
		Name:       openstack.CloudProviderConfigName,
		EmbeddedFS: charts.InternalChart,
		Path:       filepath.Join(charts.InternalChartsPath, openstack.CloudProviderConfigName),
		Objects: []*chart.Object{
			{Type: &corev1.Secret{}, Name: openstack.CloudProviderConfigName},
			{Type: &corev1.Secret{}, Name: openstack.CloudProviderDiskConfigName},
		},
	}

	controlPlaneChart = &chart.Chart{
		Name:       "seed-controlplane",
		EmbeddedFS: charts.InternalChart,
		Path:       filepath.Join(charts.InternalChartsPath, "seed-controlplane"),
		SubCharts: []*chart.Chart{
			{
				Name:   openstack.CloudControllerManagerName,
				Images: []string{imagevector.ImageNameCloudControllerManager},
				Objects: []*chart.Object{
					{Type: &corev1.Service{}, Name: openstack.CloudControllerManagerName},
					{Type: &appsv1.Deployment{}, Name: openstack.CloudControllerManagerName},
					{Type: &vpaautoscalingv1.VerticalPodAutoscaler{}, Name: openstack.CloudControllerManagerName + "-vpa"},
				},
			},
			{
				Name:   openstack.STACKITCloudControllerManagerName,
				Images: []string{openstack.STACKITCloudControllerManagerImageName},
				Objects: []*chart.Object{
					{Type: &appsv1.Deployment{}, Name: openstack.STACKITCloudControllerManagerName},
					{Type: &corev1.ConfigMap{}, Name: openstack.STACKITCloudControllerManagerName},
					{Type: &vpaautoscalingv1.VerticalPodAutoscaler{}, Name: openstack.STACKITCloudControllerManagerImageName + "-vpa"},
				},
			},
			{
				Name: openstack.CSIControllerName,
				Images: []string{
					imagevector.ImageNameCsiDriverCinder,
					imagevector.ImageNameCsiProvisioner,
					imagevector.ImageNameCsiAttacher,
					imagevector.ImageNameCsiSnapshotter,
					imagevector.ImageNameCsiResizer,
					imagevector.ImageNameCsiLivenessProbe,
					imagevector.ImageNameCsiSnapshotController,
				},
				Objects: []*chart.Object{
					// csi-driver-controller
					{Type: &appsv1.Deployment{}, Name: openstack.CSIControllerName},
					{Type: &vpaautoscalingv1.VerticalPodAutoscaler{}, Name: openstack.CSIControllerName + "-vpa"},
					// csi-snapshot-controller
					{Type: &appsv1.Deployment{}, Name: openstack.CSISnapshotControllerName},
					{Type: &vpaautoscalingv1.VerticalPodAutoscaler{}, Name: openstack.CSISnapshotControllerName + "-vpa"},
				},
			},
			{
				Name: openstack.CSISTACKITControllerName,
				Images: []string{
					imagevector.ImageNameCsiDriverStackit,
					imagevector.ImageNameCsiProvisioner,
					imagevector.ImageNameCsiAttacher,
					imagevector.ImageNameCsiSnapshotter,
					imagevector.ImageNameCsiResizer,
					imagevector.ImageNameCsiLivenessProbe,
					imagevector.ImageNameCsiSnapshotController,
				},
				Objects: []*chart.Object{
					// csi-driver-controller
					{Type: &appsv1.Deployment{}, Name: CSIStackitPrefix + "-csi-driver-controller"},
					{Type: &vpaautoscalingv1.VerticalPodAutoscaler{}, Name: CSIStackitPrefix + "-csi-driver-vpa"},
					{Type: &corev1.Secret{}, Name: CSIStackitPrefix + "-cloud-provider-config"},
					// csi-snapshot-controller
					{Type: &appsv1.Deployment{}, Name: CSIStackitPrefix + "-csi-snapshot-controller"},
					{Type: &vpaautoscalingv1.VerticalPodAutoscaler{}, Name: CSIStackitPrefix + "-csi-snapshot-controller-vpa"},
				},
			},
			{
				Name:   openstack.STACKITALBControllerManagerName,
				Images: []string{imagevector.ImageNameStackitAlbControllerManager},
				Objects: []*chart.Object{
					// stackit-alb-controller-manager
					{Type: &appsv1.Deployment{}, Name: openstack.STACKITALBControllerManagerName},
					{Type: &vpaautoscalingv1.VerticalPodAutoscaler{}, Name: openstack.STACKITALBControllerManagerName},
				},
			},
		},
	}

	controlPlaneShootChart = &chart.Chart{
		Name:       "shoot-system-components",
		EmbeddedFS: charts.InternalChart,
		Path:       filepath.Join(charts.InternalChartsPath, "shoot-system-components"),
		SubCharts: []*chart.Chart{
			{
				Name: openstack.CloudControllerManagerName,
				Objects: []*chart.Object{
					{Type: &rbacv1.ClusterRole{}, Name: "system:controller:cloud-node-controller"},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: "system:controller:cloud-node-controller"},
				},
			},
			{
				Name: openstack.CSISTACKITNodeName,
				Images: []string{
					imagevector.ImageNameCsiDriverStackit,
					imagevector.ImageNameCsiNodeDriverRegistrar,
					imagevector.ImageNameCsiLivenessProbe,
				},
				Objects: []*chart.Object{
					// csi-driver
					{Type: &appsv1.DaemonSet{}, Name: fmt.Sprintf("%s-csi-driver-node", CSIStackitPrefix)},
					{Type: &storagev1.CSIDriver{}, Name: openstack.CSISTACKITStorageProvisioner},
					{Type: &corev1.ServiceAccount{}, Name: fmt.Sprintf("%s-csi-driver-node", CSIStackitPrefix)},
					{Type: &corev1.Secret{}, Name: fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CloudProviderConfigName)},
					{Type: &rbacv1.ClusterRole{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIDriverName)},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIDriverName)},
					// csi-provisioner
					{Type: &rbacv1.ClusterRole{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIProvisionerName)},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIProvisionerName)},
					{Type: &rbacv1.Role{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIProvisionerName)},
					{Type: &rbacv1.RoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIProvisionerName)},
					// csi-attacher
					{Type: &rbacv1.ClusterRole{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIAttacherName)},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIAttacherName)},
					{Type: &rbacv1.Role{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIAttacherName)},
					{Type: &rbacv1.RoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIAttacherName)},
					// csi-snapshot-controller
					{Type: &rbacv1.ClusterRole{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSISnapshotControllerName)},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSISnapshotControllerName)},
					{Type: &rbacv1.Role{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSISnapshotControllerName)},
					{Type: &rbacv1.RoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSISnapshotControllerName)},
					// csi-snapshotter
					{Type: &rbacv1.ClusterRole{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSISnapshotterName)},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSISnapshotterName)},
					{Type: &rbacv1.Role{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSISnapshotterName)},
					{Type: &rbacv1.RoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSISnapshotterName)},
					// csi-resizer
					{Type: &rbacv1.ClusterRole{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIResizerName)},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIResizerName)},
					{Type: &rbacv1.Role{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIResizerName)},
					{Type: &rbacv1.RoleBinding{}, Name: fmt.Sprintf("%s:%s", CSIStackitPrefix, openstack.CSIResizerName)},
				},
			},
			{
				Name: openstack.CSINodeName,
				Images: []string{
					imagevector.ImageNameCsiDriverCinder,
					imagevector.ImageNameCsiNodeDriverRegistrar,
					imagevector.ImageNameCsiLivenessProbe,
				},
				Objects: []*chart.Object{
					// csi-driver
					{Type: &appsv1.DaemonSet{}, Name: openstack.CSINodeName},
					{Type: &storagev1.CSIDriver{}, Name: openstack.CSIStorageProvisioner},
					{Type: &corev1.ServiceAccount{}, Name: openstack.CSIDriverName},
					{Type: &corev1.Secret{}, Name: openstack.CloudProviderConfigName},
					{Type: &rbacv1.ClusterRole{}, Name: openstack.UsernamePrefix + openstack.CSIDriverName},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSIDriverName},
					// csi-provisioner
					{Type: &rbacv1.ClusterRole{}, Name: openstack.UsernamePrefix + openstack.CSIProvisionerName},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSIProvisionerName},
					{Type: &rbacv1.Role{}, Name: openstack.UsernamePrefix + openstack.CSIProvisionerName},
					{Type: &rbacv1.RoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSIProvisionerName},
					// csi-attacher
					{Type: &rbacv1.ClusterRole{}, Name: openstack.UsernamePrefix + openstack.CSIAttacherName},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSIAttacherName},
					{Type: &rbacv1.Role{}, Name: openstack.UsernamePrefix + openstack.CSIAttacherName},
					{Type: &rbacv1.RoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSIAttacherName},
					// csi-snapshot-controller
					{Type: &rbacv1.ClusterRole{}, Name: openstack.UsernamePrefix + openstack.CSISnapshotControllerName},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSISnapshotControllerName},
					{Type: &rbacv1.Role{}, Name: openstack.UsernamePrefix + openstack.CSISnapshotControllerName},
					{Type: &rbacv1.RoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSISnapshotControllerName},
					// csi-snapshotter
					{Type: &rbacv1.ClusterRole{}, Name: openstack.UsernamePrefix + openstack.CSISnapshotterName},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSISnapshotterName},
					{Type: &rbacv1.Role{}, Name: openstack.UsernamePrefix + openstack.CSISnapshotterName},
					{Type: &rbacv1.RoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSISnapshotterName},
					// csi-resizer
					{Type: &rbacv1.ClusterRole{}, Name: openstack.UsernamePrefix + openstack.CSIResizerName},
					{Type: &rbacv1.ClusterRoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSIResizerName},
					{Type: &rbacv1.Role{}, Name: openstack.UsernamePrefix + openstack.CSIResizerName},
					{Type: &rbacv1.RoleBinding{}, Name: openstack.UsernamePrefix + openstack.CSIResizerName},
				},
			},
		},
	}

	controlPlaneShootCRDsChart = &chart.Chart{
		Name:       "shoot-crds",
		EmbeddedFS: charts.InternalChart,
		Path:       filepath.Join(charts.InternalChartsPath, "shoot-crds"),
		SubCharts: []*chart.Chart{
			{
				Name: "volumesnapshots",
				Objects: []*chart.Object{
					{Type: &apiextensionsv1.CustomResourceDefinition{}, Name: "volumesnapshotclasses.snapshot.storage.k8s.io"},
					{Type: &apiextensionsv1.CustomResourceDefinition{}, Name: "volumesnapshotcontents.snapshot.storage.k8s.io"},
					{Type: &apiextensionsv1.CustomResourceDefinition{}, Name: "volumesnapshots.snapshot.storage.k8s.io"},
				},
			},
		},
	}

	storageClassChart = &chart.Chart{
		Name:       "shoot-storageclasses",
		EmbeddedFS: charts.InternalChart,
		Path:       filepath.Join(charts.InternalChartsPath, "shoot-storageclasses"),
	}
)

// NewValuesProvider creates a new ValuesProvider for the generic actuator.
func NewValuesProvider(mgr manager.Manager, deployALBIngressController bool, customLabelDomain string) genericactuator.ValuesProvider {
	return &valuesProvider{
		client:                     mgr.GetClient(),
		decoder:                    serializer.NewCodecFactory(mgr.GetScheme(), serializer.EnableStrict).UniversalDecoder(),
		deployALBIngressController: deployALBIngressController,
		customLabelDomain:          customLabelDomain,
	}
}

// valuesProvider is a ValuesProvider that provides OpenStack-specific values for the 2 charts applied by the generic actuator.
type valuesProvider struct {
	genericactuator.NoopValuesProvider
	client                     k8sclient.Client
	decoder                    runtime.Decoder
	deployALBIngressController bool
	customLabelDomain          string
}

// GetConfigChartValues returns the values for the config chart applied by the generic actuator.
func (vp *valuesProvider) GetConfigChartValues(
	ctx context.Context,
	cp *extensionsv1alpha1.ControlPlane,
	cluster *extensionscontroller.Cluster,
) (map[string]any, error) {
	controlPlaneConfig := &stackitv1alpha1.ControlPlaneConfig{}
	if cp.Spec.ProviderConfig != nil {
		if _, _, err := vp.decoder.Decode(cp.Spec.ProviderConfig.Raw, nil, controlPlaneConfig); err != nil {
			return nil, fmt.Errorf("could not decode providerConfig of controlplane '%s': %w", k8sclient.ObjectKeyFromObject(cp), err)
		}
	}

	infraStatus, err := vp.getInfrastructureStatus(cp)
	if err != nil {
		return nil, err
	}

	cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster)
	if err != nil {
		return nil, err
	}

	var osCredentials *openstack.Credentials
	if !isSTACKITOnly(cluster, controlPlaneConfig) {
		osCredentials, err = openstack.GetCredentials(ctx, vp.client, cp.Spec.SecretRef, false)
		if err != nil {
			return nil, fmt.Errorf("could not get service account from secret '%s/%s': %w", cp.Spec.SecretRef.Namespace, cp.Spec.SecretRef.Name, err)
		}
	}

	// We ONLY enable the cloud-controller-manager's route-controller when overlay: false AND
	// the backend is NOT an BGP enabled backend.
	// BECAUSE BGP will propagate routes locally on the nodes via BGP instead of using the Router (static routes)
	overlayEnabled, err := vp.isOverlayEnabled(cluster.Shoot.Spec.Networking)
	if err != nil {
		return nil, fmt.Errorf("could not determine overlay status: %v", err)
	}
	BGPEnabled, err := vp.isBGPEnabled(cluster.Shoot.Spec.Networking)
	if err != nil {
		return nil, fmt.Errorf("could not determine if BGP is enabled: %v", err)
	}
	useRouteController := !overlayEnabled && !BGPEnabled

	return getConfigChartValues(infraStatus, cloudProfileConfig, controlPlaneConfig, cluster, cp, osCredentials, useRouteController)
}

func (vp *valuesProvider) getInfrastructureStatus(cp *extensionsv1alpha1.ControlPlane) (*stackitv1alpha1.InfrastructureStatus, error) {
	infraStatus := &stackitv1alpha1.InfrastructureStatus{}
	if _, _, err := vp.decoder.Decode(cp.Spec.InfrastructureProviderStatus.Raw, nil, infraStatus); err != nil {
		return nil, fmt.Errorf("could not decode infrastructureProviderStatus of controlplane '%s': %w", k8sclient.ObjectKeyFromObject(cp), err)
	}
	return infraStatus, nil
}

// GetControlPlaneChartValues returns the values for the control plane chart applied by the generic actuator.
func (vp *valuesProvider) GetControlPlaneChartValues(
	ctx context.Context,
	cp *extensionsv1alpha1.ControlPlane,
	cluster *extensionscontroller.Cluster,
	secretsReader secretsmanager.Reader,
	checksums map[string]string,
	scaledDown bool,
) (
	map[string]any,
	error,
) {
	// Decode providerConfig
	cpConfig := &stackitv1alpha1.ControlPlaneConfig{}
	if cp.Spec.ProviderConfig != nil {
		if _, _, err := vp.decoder.Decode(cp.Spec.ProviderConfig.Raw, nil, cpConfig); err != nil {
			return nil, fmt.Errorf("could not decode providerConfig of controlplane '%s': %w", k8sclient.ObjectKeyFromObject(cp), err)
		}
	}

	// TODO(timuthy): Delete this in a future release.
	if err := kutil.DeleteObject(ctx, vp.client, &networkingv1.NetworkPolicy{ObjectMeta: metav1.ObjectMeta{Name: "allow-kube-apiserver-to-csi-snapshot-validation", Namespace: cp.Namespace}}); err != nil {
		return nil, fmt.Errorf("failed deleting legacy csi-snapshot-validation network policy: %w", err)
	}

	// TODO: rm in future release.
	if err := cleanupSeedLegacyCSISnapshotValidation(ctx, vp.client, cp.Namespace); err != nil {
		return nil, err
	}
	if err := cleanupCloudProviderConfigSecret(ctx, vp.client, cp.Namespace); err != nil {
		return nil, err
	}

	cpConfigSecret := &corev1.Secret{}

	if err := vp.client.Get(ctx, k8sclient.ObjectKey{Namespace: cp.Namespace, Name: openstack.CloudProviderConfigName}, cpConfigSecret); err != nil {
		return nil, err
	}
	checksums[openstack.CloudProviderConfigName] = gardenerutils.ComputeChecksum(cpConfigSecret.Data)

	var userAgentHeaders []string
	cpDiskConfigSecret := &corev1.Secret{}
	if err := vp.client.Get(ctx, k8sclient.ObjectKey{Namespace: cp.Namespace, Name: openstack.CloudProviderCSIDiskConfigName}, cpDiskConfigSecret); err != nil {
		return nil, err
	}
	checksums[openstack.CloudProviderCSIDiskConfigName] = gardenerutils.ComputeChecksum(cpDiskConfigSecret.Data)
	credentials, _ := vp.getCredentials(ctx, cp) // ignore missing credentials

	stackitCredentials, err := vp.getSTACKITCredentials(ctx, cp) // ignore missing credentials
	if err != nil {
		return nil, fmt.Errorf("getting STACKIT credentials: %w", err)
	}
	userAgentHeaders = vp.getUserAgentHeaders(credentials, cluster)

	infra, err := vp.getInfrastructureStatus(cp)
	if err != nil {
		return nil, err
	}

	cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster)
	if err != nil {
		return nil, err
	}

	return vp.getControlPlaneChartValues(ctx, cpConfig, cp, cluster, infra, secretsReader, userAgentHeaders, checksums, scaledDown, stackitCredentials, cloudProfileConfig.APIEndpoints)
}

// GetControlPlaneShootChartValues returns the values for the control plane shoot chart applied by the generic actuator.
func (vp *valuesProvider) GetControlPlaneShootChartValues(
	ctx context.Context,
	cp *extensionsv1alpha1.ControlPlane,
	cluster *extensionscontroller.Cluster,
	_ secretsmanager.Reader,
	_ map[string]string,
) (map[string]any, error) {
	// Decode providerConfig
	cpConfig := &stackitv1alpha1.ControlPlaneConfig{}
	if cp.Spec.ProviderConfig != nil {
		if _, _, err := vp.decoder.Decode(cp.Spec.ProviderConfig.Raw, nil, cpConfig); err != nil {
			return nil, fmt.Errorf("could not decode providerConfig of controlplane '%s': %w", k8sclient.ObjectKeyFromObject(cp), err)
		}
	}

	cloudProfileConfig, err := helper.CloudProfileConfigFromCluster(cluster)
	if err != nil {
		return nil, err
	}
	return vp.getControlPlaneShootChartValues(ctx, cpConfig, cp, cloudProfileConfig, cluster)
}

// GetStorageClassesChartValues returns the values for the shoot storageclasses chart applied by the generic actuator.
func (vp *valuesProvider) GetStorageClassesChartValues(
	_ context.Context,
	controlPlane *extensionsv1alpha1.ControlPlane,
	cluster *extensionscontroller.Cluster,
) (map[string]any, error) {
	providerConfig := stackitv1alpha1.CloudProfileConfig{}
	if cluster.CloudProfile.Spec.ProviderConfig != nil {
		if _, _, err := vp.decoder.Decode(cluster.CloudProfile.Spec.ProviderConfig.Raw, nil, &providerConfig); err != nil {
			return nil, fmt.Errorf("could not decode providerConfig of controlplane '%s': %w", k8sclient.ObjectKeyFromObject(controlPlane), err)
		}
	}
	// Decode providerConfig for determining the CSI driver in use
	cpConfig := &stackitv1alpha1.ControlPlaneConfig{}
	if controlPlane.Spec.ProviderConfig != nil {
		if _, _, err := vp.decoder.Decode(controlPlane.Spec.ProviderConfig.Raw, nil, cpConfig); err != nil {
			return nil, fmt.Errorf("could not decode providerConfig of controlplane '%s': %w", k8sclient.ObjectKeyFromObject(controlPlane), err)
		}
	}

	values := make(map[string]any)
	if len(providerConfig.StorageClasses) != 0 {
		allSc := make([]map[string]any, len(providerConfig.StorageClasses))
		for i, sc := range providerConfig.StorageClasses {
			storageClassValues := map[string]any{
				"name": sc.Name,
			}

			if sc.Default != nil && *sc.Default {
				storageClassValues["default"] = true
			}

			if len(sc.Annotations) != 0 {
				storageClassValues["annotations"] = sc.Annotations
			}
			if len(sc.Labels) != 0 {
				storageClassValues["labels"] = sc.Labels
			}
			if len(sc.Parameters) != 0 {
				storageClassValues["parameters"] = sc.Parameters
			}

			csiDriverInUse := getCSIDriver(cpConfig)
			switch csiDriverInUse {
			case stackitv1alpha1.OPENSTACK:
				storageClassValues["provisioner"] = openstack.CSIStorageProvisioner
			case stackitv1alpha1.STACKIT:
				storageClassValues["provisioner"] = openstack.CSISTACKITStorageProvisioner
			default:
				storageClassValues["provisioner"] = sc.Provisioner
			}

			if sc.ReclaimPolicy != nil && *sc.ReclaimPolicy != "" {
				storageClassValues["reclaimPolicy"] = sc.ReclaimPolicy
			}

			if sc.VolumeBindingMode != nil && *sc.VolumeBindingMode != "" {
				storageClassValues["volumeBindingMode"] = sc.VolumeBindingMode
			}

			allSc[i] = storageClassValues
		}
		values["storageclasses"] = allSc
		return values, nil
	}

	storageclasses := []map[string]any{
		{
			"name":              "default",
			"default":           true,
			"provisioner":       openstack.CSIStorageProvisioner,
			"volumeBindingMode": storagev1.VolumeBindingWaitForFirstConsumer,
		},
		{
			"name":              "default-class",
			"provisioner":       openstack.CSIStorageProvisioner,
			"volumeBindingMode": storagev1.VolumeBindingWaitForFirstConsumer,
		},
	}

	values["storageclasses"] = storageclasses

	return values, nil
}

func (vp *valuesProvider) getCredentials(ctx context.Context, cp *extensionsv1alpha1.ControlPlane) (*openstack.Credentials, error) {
	return openstack.GetCredentials(ctx, vp.client, cp.Spec.SecretRef, false)
}

func (vp *valuesProvider) getSTACKITCredentials(ctx context.Context, cp *extensionsv1alpha1.ControlPlane) (*stackit.Credentials, error) {
	return stackit.GetCredentialsFromSecretRef(ctx, vp.client, cp.Spec.SecretRef)
}

func (vp *valuesProvider) getUserAgentHeaders(
	credentials *openstack.Credentials,
	cluster *extensionscontroller.Cluster,
) []string {
	headers := []string{}

	// Add the domain and project/tenant to the useragent headers if the secret
	// could be read and the respective fields in secret are not empty.
	if credentials != nil {
		if credentials.DomainName != "" {
			headers = append(headers, credentials.DomainName)
		}
		if credentials.TenantName != "" {
			headers = append(headers, credentials.TenantName)
		}
	}

	if cluster.Shoot != nil {
		headers = append(headers, cluster.Shoot.Status.TechnicalID)
	}

	return headers
}

// getConfigChartValues collects and returns the configuration chart values.
func getConfigChartValues(
	infraStatus *stackitv1alpha1.InfrastructureStatus,
	cloudProfileConfig *stackitv1alpha1.CloudProfileConfig,
	controlPlaneConfig *stackitv1alpha1.ControlPlaneConfig,
	cluster *extensionscontroller.Cluster,
	cp *extensionsv1alpha1.ControlPlane,
	osCredentials *openstack.Credentials,
	addRouterID bool,
) (map[string]any, error) {
	if cloudProfileConfig == nil {
		return nil, fmt.Errorf("cloud profile config is nil - cannot determine keystone URL and other parameters")
	}

	values := map[string]any{}
	if isSTACKITOnly(cluster, controlPlaneConfig) {
		values["stackitonly"] = true
	} else {
		values["domainName"] = osCredentials.DomainName
		values["tenantName"] = osCredentials.TenantName
		values["username"] = osCredentials.Username
		values["password"] = osCredentials.Password
		values["insecure"] = osCredentials.Insecure
		values["authUrl"] = osCredentials.AuthURL
		values["applicationCredentialID"] = osCredentials.ApplicationCredentialID
		values["applicationCredentialName"] = osCredentials.ApplicationCredentialName
		values["applicationCredentialSecret"] = osCredentials.ApplicationCredentialSecret
		values["region"] = cp.Spec.Region
		values["requestTimeout"] = cloudProfileConfig.RequestTimeout
		values["ignoreVolumeAZ"] = cloudProfileConfig.IgnoreVolumeAZ != nil && *cloudProfileConfig.IgnoreVolumeAZ
		// detect internal network.
		// See https://github.com/kubernetes/cloud-provider-openstack/blob/v1.22.1/docs/openstack-cloud-controller-manager/using-openstack-cloud-controller-manager.md#networking
		values["internalNetworkName"] = infraStatus.Networks.Name

		if addRouterID {
			values["routerID"] = infraStatus.Networks.Router.ID
		}

		if len(osCredentials.CACert) > 0 {
			values["caCert"] = osCredentials.CACert
		}
	}

	return values, nil
}

// getControlPlaneChartValues collects and returns the control plane chart values.
func (vp *valuesProvider) getControlPlaneChartValues(ctx context.Context, cpConfig *stackitv1alpha1.ControlPlaneConfig, cp *extensionsv1alpha1.ControlPlane, cluster *extensionscontroller.Cluster, infra *stackitv1alpha1.InfrastructureStatus, secretsReader secretsmanager.Reader, userAgentHeaders []string, checksums map[string]string, scaledDown bool, stackitCredentials *stackit.Credentials, apiEndpoints *stackitv1alpha1.APIEndpoints) (map[string]any, error) {
	controlPlaneValues := make(map[string]any)
	ccm, err := getCCMChartValues(cpConfig, cp, cluster, secretsReader, userAgentHeaders, checksums, scaledDown)
	if err != nil {
		return nil, err
	}

	// If emergency loadbalancer access is requested by placing the [LoadBalancerEmergencyAccessSecretName] secret
	// in the shoot controlplane namespace, the CCM must be reconfigured to bypass the LB API gateway and
	// hit the API on the URL and with the token which are both specified by the secret.
	// See ADR: https://developers.stackit.schwarz/domains/runtime/ske/architecture/adrs/loadbalancer-emergency-access/
	lbAPIURL, lbAPIToken, err := vp.checkEmergencyLoadBalancerAccess(ctx, types.NamespacedName{
		Name:      LoadBalancerEmergencyAccessSecretName,
		Namespace: cp.Namespace,
	})
	if err != nil {
		return nil, err
	}

	stackitCredentialsConfig := stackitCredentials

	// Copy API endpoints to avoid mutating the original from CloudProfileConfig
	var ccmAPIEndpoints stackitv1alpha1.APIEndpoints
	if apiEndpoints != nil {
		ccmAPIEndpoints = *apiEndpoints
	}

	// Override with emergency LB API access if configured
	if lbAPIURL != "" && lbAPIToken != "" {
		ccmAPIEndpoints.LoadBalancer = &lbAPIURL
		ccmAPIEndpoints.TokenEndpoint = nil
		stackitCredentialsConfig.LoadBalancerAPIEmergencyToken = lbAPIToken
	}

	stackitRegion := stackit.DetermineRegion(cluster)
	stackitccm, err := getSTACKITCCMChartValues(cpConfig, cp, cluster, infra, stackitCredentialsConfig, stackitRegion, &ccmAPIEndpoints, checksums, scaledDown, vp.customLabelDomain)
	if err != nil {
		return nil, err
	}
	if stackitccm == nil {
		// NOTE: ensure deletion of STACKIT CCM deployment, if not enabled
		if err := vp.deleteControlPlaneComponentsForGivenChart(ctx, cp.Namespace, openstack.STACKITCloudControllerManagerName); err != nil {
			return nil, err
		}
	}

	storageCSIDriver := getCSIDriver(cpConfig)
	switch storageCSIDriver {
	case stackitv1alpha1.OPENSTACK:
		csiCinder := getCSIControllerChartValues(cluster, userAgentHeaders, checksums, scaledDown)
		controlPlaneValues[openstack.CSIControllerName] = csiCinder
		controlPlaneValues[openstack.CSISTACKITControllerName] = map[string]any{
			"enabled": false,
		}
	case stackitv1alpha1.STACKIT:
		csiSTACKIT := getCSISTACKITControllerChartValues(cluster, stackitCredentialsConfig, userAgentHeaders, checksums, scaledDown, apiEndpoints, vp.customLabelDomain)
		controlPlaneValues[openstack.CSISTACKITControllerName] = csiSTACKIT
		controlPlaneValues[openstack.CSIControllerName] = map[string]any{
			"enabled": false,
		}
	default:
		return nil, fmt.Errorf("unsupported storage CSI Driver: %s", storageCSIDriver)
	}

	maps.Copy(controlPlaneValues, map[string]any{
		"global": map[string]any{
			"genericTokenKubeconfigSecretName": extensionscontroller.GenericTokenKubeconfigSecretNameFromCluster(cluster),
		},
		openstack.CloudControllerManagerName:        ccm,
		openstack.STACKITCloudControllerManagerName: stackitccm,
	})

	if vp.deployALBIngressController {
		fmt.Println("deploying ALB Ingress Controller")
		albcm, err := getSTACKITALBCMChartValues(cpConfig, cluster, infra, stackitCredentialsConfig, apiEndpoints, scaledDown, stackitRegion)
		if err != nil {
			return nil, err
		}

		controlPlaneValues[openstack.STACKITALBControllerManagerName] = albcm
	} else {
		// NOTE: ensure deletion of ALB deployment, if disabled
		if err := vp.deleteControlPlaneComponentsForGivenChart(ctx, cp.Namespace, openstack.STACKITALBControllerManagerName); err != nil {
			return nil, err
		}
	}

	return controlPlaneValues, nil
}

func (vp *valuesProvider) cleanupControlPlaneFromUnusedCSIDriverComponents(ctx context.Context, namespace string, csiDriver stackitv1alpha1.ControllerName) error {
	switch csiDriver {
	case stackitv1alpha1.STACKIT:
		err := vp.deleteControlPlaneComponentsForGivenChart(ctx, namespace, openstack.CSIControllerName)
		if err != nil {
			return err
		}
	case stackitv1alpha1.OPENSTACK:
		err := vp.deleteControlPlaneComponentsForGivenChart(ctx, namespace, openstack.CSISTACKITControllerName)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported CSI Driver: %s", csiDriver)
	}

	return nil
}

func (vp *valuesProvider) deleteControlPlaneComponentsForGivenChart(ctx context.Context, namespace string, chartName string) error {
	for _, subchart := range controlPlaneChart.SubCharts {
		if subchart.Name == chartName {
			for _, obj := range subchart.Objects {
				objToDelete := obj.Type
				objToDelete.SetNamespace(namespace)
				objToDelete.SetName(obj.Name)

				err := vp.client.Delete(ctx, objToDelete)
				if errors.IsNotFound(err) {
					continue
				}
				if err != nil {
					return fmt.Errorf("failed to delete object %s: %v", obj.Name, err)
				}
			}
		}
	}
	return nil
}

// getCCMControllersForSTACKIT determines the correct number of controller to start for the STACKIT CCM
// In the case of "stackit" the controller needs to be spawned with all controllers (service, node, lifecycle-node)
// since openstack controller will not be running.
// In the case of "openstack" the controller MUST only be running with the service controller enabled.
func getCCMControllersForSTACKIT(cpConfig *stackitv1alpha1.ControlPlaneConfig) []string {
	stackitCCM := getCCMController(cpConfig) == stackitv1alpha1.STACKIT
	if stackitCCM {
		// If STACKIT CCM then deploy everything
		return []string{"*"}
	}
	return []string{STACKITCCMServiceLoadbalancerController}
}

// getSTACKITCCMChartValues collects and returns the CCM chart values.
func getSTACKITCCMChartValues(
	cpConfig *stackitv1alpha1.ControlPlaneConfig,
	_ *extensionsv1alpha1.ControlPlane,
	cluster *extensionscontroller.Cluster,
	infra *stackitv1alpha1.InfrastructureStatus,
	credentials *stackit.Credentials,
	stackitRegion string,
	apiEndpoints *stackitv1alpha1.APIEndpoints,
	checksums map[string]string,
	scaledDown bool,
	customLabelDomain string,
) (map[string]any, error) {
	if credentials == nil {
		return nil, fmt.Errorf("no STACKIT credentials are provided in cluster %s", cluster.Shoot.Name)
	}

	ccmConfig := map[string]any{
		"stackitNetworkID": infra.Networks.ID,
		"stackitRegion":    stackitRegion,
		"stackitProjectID": credentials.ProjectID,
		"extraLabels": map[string]string{
			// TODO: migrate away from the old key
			STACKITLBClusterLabelKey:                 cluster.Shoot.Status.TechnicalID,
			utils.ClusterLabelKey(customLabelDomain): cluster.Shoot.Status.TechnicalID,
		},
		"customLabelDomain": customLabelDomain,
	}

	if credentials.LoadBalancerAPIEmergencyToken != "" {
		ccmConfig["loadBalancerEmergencyToken"] = credentials.LoadBalancerAPIEmergencyToken
	}

	if apiEndpoints != nil {
		if apiEndpoints.LoadBalancer != nil {
			ccmConfig["loadBalancerApiUrl"] = *apiEndpoints.LoadBalancer
		}
		if apiEndpoints.IaaS != nil {
			ccmConfig["iaasApiUrl"] = *apiEndpoints.IaaS
		}
		if apiEndpoints.TokenEndpoint != nil {
			ccmConfig["tokenUrl"] = *apiEndpoints.TokenEndpoint
		}
	}

	values := map[string]any{
		"enabled":     true,
		"replicas":    extensionscontroller.GetControlPlaneReplicas(cluster, scaledDown, 1),
		"technicalID": cluster.Shoot.Status.TechnicalID,
		"config":      ccmConfig,
		"controllers": getCCMControllersForSTACKIT(cpConfig),
		"podAnnotations": map[string]any{
			"checksum/secret-" + v1beta1constants.SecretNameCloudProvider:         checksums[v1beta1constants.SecretNameCloudProvider],
			"checksum/config-" + openstack.STACKITCloudControllerManagerImageName: gardenerutils.ComputeChecksum(ccmConfig),
		},
		"podLabels": map[string]any{
			v1beta1constants.LabelPodMaintenanceRestart: "true",
		},
	}

	if cpConfig.CloudControllerManager != nil {
		values["featureGates"] = cpConfig.CloudControllerManager.FeatureGates
	}

	return values, nil
}

// getCCMChartValues collects and returns the CCM chart values.
func getCCMChartValues(
	cpConfig *stackitv1alpha1.ControlPlaneConfig,
	_ *extensionsv1alpha1.ControlPlane,
	cluster *extensionscontroller.Cluster,
	secretsReader secretsmanager.Reader,
	userAgentHeaders []string,
	checksums map[string]string,
	scaledDown bool,
) (map[string]any, error) {
	serverSecret, found := secretsReader.Get(cloudControllerManagerServerName)
	if !found {
		return nil, fmt.Errorf("secret %q not found", cloudControllerManagerServerName)
	}

	enabled := getCCMController(cpConfig) == stackitv1alpha1.OPENSTACK

	values := map[string]any{
		"enabled":           enabled,
		"replicas":          extensionscontroller.GetControlPlaneReplicas(cluster, scaledDown, 1),
		"technicalID":       cluster.Shoot.Status.TechnicalID,
		"kubernetesVersion": cluster.Shoot.Spec.Kubernetes.Version,
		"podNetwork":        strings.Join(extensionscontroller.GetPodNetwork(cluster), ","),
		"podAnnotations": map[string]any{
			"checksum/secret-" + v1beta1constants.SecretNameCloudProvider: checksums[v1beta1constants.SecretNameCloudProvider],
			"checksum/secret-" + openstack.CloudProviderConfigName:        checksums[openstack.CloudProviderConfigName],
		},
		"podLabels": map[string]any{
			v1beta1constants.LabelPodMaintenanceRestart: "true",
		},
		"tlsCipherSuites": kutil.TLSCipherSuites,
		"secrets": map[string]any{
			"server": serverSecret.Name,
		},
	}

	if userAgentHeaders != nil {
		values["userAgentHeaders"] = userAgentHeaders
	}

	if cpConfig.CloudControllerManager != nil {
		values["featureGates"] = cpConfig.CloudControllerManager.FeatureGates
	}

	return values, nil
}

func getCSISTACKITControllerChartValues(cluster *extensionscontroller.Cluster, credentials *stackit.Credentials, userAgentHeaders []string, checksums map[string]string, scaledDown bool, apiEndpoints *stackitv1alpha1.APIEndpoints, customLabelDomain string) map[string]any {
	region := stackit.DetermineRegion(cluster)

	endpointConfig := map[string]string{}
	if apiEndpoints != nil {
		if apiEndpoints.TokenEndpoint != nil {
			endpointConfig["tokenUrl"] = *apiEndpoints.TokenEndpoint
		}
		if apiEndpoints.IaaS != nil {
			endpointConfig["iaasUrl"] = *apiEndpoints.IaaS
		}
	}

	values := map[string]any{
		"enabled":   true,
		"projectID": credentials.ProjectID,
		"region":    region,
		"replicas":  extensionscontroller.GetControlPlaneReplicas(cluster, scaledDown, 1),
		"podAnnotations": map[string]any{
			"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
		},
		"csiSnapshotController": map[string]any{
			"replicas": extensionscontroller.GetControlPlaneReplicas(cluster, scaledDown, 1),
		},
		"stackitEndpoints":  endpointConfig,
		"customLabelDomain": customLabelDomain,
	}
	if userAgentHeaders != nil {
		values["userAgentHeaders"] = userAgentHeaders
	}
	return values
}

// getCSIControllerChartValues collects and returns the CSIController chart values.
func getCSIControllerChartValues(cluster *extensionscontroller.Cluster, userAgentHeaders []string, checksums map[string]string, scaledDown bool) map[string]any {
	values := map[string]any{
		"kubernetesVersion": cluster.Shoot.Spec.Kubernetes.Version,
		"enabled":           true,
		"replicas":          extensionscontroller.GetControlPlaneReplicas(cluster, scaledDown, 1),
		"podAnnotations": map[string]any{
			"checksum/secret-" + openstack.CloudProviderCSIDiskConfigName: checksums[openstack.CloudProviderCSIDiskConfigName],
		},
		"csiSnapshotController": map[string]any{
			"replicas": extensionscontroller.GetControlPlaneReplicas(cluster, scaledDown, 1),
		},
		"maxEntries": 1000,
	}
	if userAgentHeaders != nil {
		values["userAgentHeaders"] = userAgentHeaders
	}
	return values
}

func getSTACKITALBCMChartValues(
	cpConfig *stackitv1alpha1.ControlPlaneConfig,
	cluster *extensionscontroller.Cluster,
	infra *stackitv1alpha1.InfrastructureStatus,
	credentials *stackit.Credentials,
	apiEndpoints *stackitv1alpha1.APIEndpoints,
	scaledDown bool,
	stackitRegion string,
) (map[string]any, error) {
	if !DeploySTACKITALB(cpConfig) {
		return nil, nil
	}

	if credentials == nil {
		return nil, fmt.Errorf("no STACKIT credentials are provided in cluster %s", cluster.Shoot.Name)
	}

	config := map[string]any{
		"region":           stackitRegion,
		"stackitProjectID": credentials.ProjectID,
	}

	if infra != nil {
		config["stackitNetworkID"] = infra.Networks.ID
	}

	if apiEndpoints != nil {
		if apiEndpoints.ApplicationLoadBalancer != nil {
			config["applicationLBApiUrl"] = apiEndpoints.ApplicationLoadBalancer
		}

		if apiEndpoints.LoadBalancerCertificate != nil {
			config["certificateApiUrl"] = *apiEndpoints.LoadBalancerCertificate
		}

		if apiEndpoints.TokenEndpoint != nil {
			config["tokenUrl"] = *apiEndpoints.TokenEndpoint
		}
	}

	values := map[string]any{
		"enabled":  true,
		"replicas": extensionscontroller.GetControlPlaneReplicas(cluster, scaledDown, 1),
		"config":   config,
	}

	return values, nil
}

func DeploySTACKITALB(cpConfig *stackitv1alpha1.ControlPlaneConfig) bool {
	return ptr.Deref(cpConfig.ApplicationLoadBalancer, stackitv1alpha1.ApplicationLoadBalancerConfig{}).Enabled
}

// getControlPlaneShootChartValues collects and returns the control plane shoot chart values.
func (vp *valuesProvider) getControlPlaneShootChartValues(ctx context.Context, cpConfig *stackitv1alpha1.ControlPlaneConfig, cp *extensionsv1alpha1.ControlPlane, cloudProfileConfig *stackitv1alpha1.CloudProfileConfig, cluster *extensionscontroller.Cluster) (map[string]any, error) {
	var csiNodeDriverValues map[string]any

	values := make(map[string]any)

	// OpenStack CSI
	csiNodeDriverValues = vp.getControlPlaneShootChartCSIValues(ctx, cpConfig, cp, cluster, cloudProfileConfig)
	// STACKIT CSI
	csiDriverSTACKITValues := vp.getControlPlaneShootChartCSISTACKITValues(ctx, cpConfig, cp, cluster, cloudProfileConfig)

	csiDriverInUse := getCSIDriver(cpConfig)
	switch csiDriverInUse {
	case stackitv1alpha1.STACKIT:
		values[openstack.CSISTACKITNodeName] = csiDriverSTACKITValues
		values[openstack.CSINodeName] = map[string]any{"enabled": false}
	case stackitv1alpha1.OPENSTACK:
		values[openstack.CSINodeName] = csiNodeDriverValues
		values[openstack.CSISTACKITNodeName] = map[string]any{"enabled": false}
	default:
		return nil, fmt.Errorf("unsupported CSI driver type: %s", csiDriverInUse)
	}

	// FIXME: Gardener doesn't track deployed components in the NewActuator. This is unlike ManagedResources, therefore
	// we must manually remove all the other components in the control-plane.
	if err := vp.cleanupControlPlaneFromUnusedCSIDriverComponents(ctx, cp.Namespace, csiDriverInUse); err != nil {
		return nil, err
	}

	maps.Copy(values, map[string]any{
		openstack.CloudControllerManagerName: map[string]any{"enabled": true},
	})

	return values, nil
}

func (vp *valuesProvider) isOverlayEnabled(network *v1beta1.Networking) (bool, error) {
	if network == nil || network.ProviderConfig == nil {
		return true, nil
	}

	// should not happen in practice because we will receive a RawExtension with Raw populated in production.
	networkProviderConfig, err := marshallNetworkProviderConfig(network)
	if err != nil {
		return false, err
	}

	var networkConfig map[string]any
	if err := json.Unmarshal(networkProviderConfig, &networkConfig); err != nil {
		return false, err
	}
	if overlay, ok := networkConfig["overlay"].(map[string]any); ok {
		return overlay["enabled"].(bool), nil
	}
	return true, nil
}

// isBGPEnabled check whether BGP is enabled in the extension. Currently only calico is supported
func (vp *valuesProvider) isBGPEnabled(network *v1beta1.Networking) (bool, error) {
	if network == nil || network.ProviderConfig == nil {
		return false, nil
	}

	// Currently only calico is supported
	if *network.Type != calico.ReleaseName {
		return false, nil
	}

	// should not happen in practice because we will receive a RawExtension with Raw populated in production.
	networkProviderConfig, err := marshallNetworkProviderConfig(network)
	if err != nil {
		return false, err
	}

	switch *network.Type {
	case calico.ReleaseName:
		networkConfig := &calicov1alpha1.NetworkConfig{}
		if _, _, err := vp.decoder.Decode(networkProviderConfig, nil, networkConfig); err != nil {
			return false, err
		}
		backend := networkConfig.Backend
		if backend == nil {
			return false, nil
		}
		return *backend == calicov1alpha1.Bird, nil
	default:
		return false, nil
	}
}

// checkEmergencyLoadBalancerAccess checks for the existence of the [LoadBalancerEmergencyAccessSecretName] secret.
// If the secret exists and is decodeable, the 'apiURL' and 'apiToken' are returned non-empty.
// If the secret doesn't exist, 'apiUrl', 'apiToken' and 'err' will be nil
// On any other cases, 'apiUrl' and 'apiToken' are empty and an error is returned.
func (vp *valuesProvider) checkEmergencyLoadBalancerAccess(ctx context.Context, secretConfKey types.NamespacedName) (apiURL, apiToken string, err error) {
	secret := &corev1.Secret{}
	err = vp.client.Get(ctx, secretConfKey, secret)
	if err != nil {
		// secret not found -> keep doing business as usual
		if errors.IsNotFound(err) {
			return "", "", nil
		}
		return "", "", err
	}

	apiURL, apiToken, err = decodeLoadBalancerAPIEmergencySecret(secret)
	if err != nil {
		return "", "", fmt.Errorf("malformed secret %s: %w", LoadBalancerEmergencyAccessSecretName, err)
	}

	return apiURL, apiToken, nil
}

// decodeLoadBalancerAPIEmergencySecret decodes a [corev1.Secret] for emergency loadbalancer access and
// returns the apiURL and apiToken to use or an error.
// The apiURL and apiToken are only set if both values exist inside the secret and are not empty.
// In case the secret is malformed (wrong key names, empty values) an error is returned.
func decodeLoadBalancerAPIEmergencySecret(secret *corev1.Secret) (apiURL string, apiToken string, err error) {
	existsNotEmpty := func(key string) (string, error) {
		value, ok := secret.Data[key]
		if !ok || len(value) == 0 {
			return "", fmt.Errorf("missing or empty secret key %s", key)
		}
		return string(value), nil
	}

	apiURL, err = existsNotEmpty(LoadBalancerEmergencyAccessAPIURLKey)
	if err != nil {
		return "", "", err
	}
	apiToken, err = existsNotEmpty(LoadBalancerEmergencyAccessAPITokenKey)
	if err != nil {
		return "", "", err
	}

	return apiURL, apiToken, nil
}

func marshallNetworkProviderConfig(network *v1beta1.Networking) ([]byte, error) {
	networkProviderConfig, err := network.ProviderConfig.MarshalJSON()
	if err != nil {
		return nil, err
	}
	if string(networkProviderConfig) == "null" {
		return nil, nil
	}
	return networkProviderConfig, nil
}

func getCSIDriver(cpConfig *stackitv1alpha1.ControlPlaneConfig) stackitv1alpha1.ControllerName {
	return stackitv1alpha1.ControllerName(cpConfig.Storage.CSI.Name)
}

func getCCMController(cpConfig *stackitv1alpha1.ControlPlaneConfig) stackitv1alpha1.ControllerName {
	return stackitv1alpha1.ControllerName(cpConfig.CloudControllerManager.Name)
}

func isSTACKITOnly(cluster *extensionscontroller.Cluster, cpConfig *stackitv1alpha1.ControlPlaneConfig) bool {
	return feature.UseStackitAPIInfrastructureController(cluster) &&
		feature.UseStackitMachineControllerManager(cluster) &&
		getCSIDriver(cpConfig) == stackitv1alpha1.STACKIT &&
		getCCMController(cpConfig) == stackitv1alpha1.STACKIT
}

func (vp *valuesProvider) getControlPlaneShootChartCSIValues(ctx context.Context, cpConfig *stackitv1alpha1.ControlPlaneConfig, cp *extensionsv1alpha1.ControlPlane, cluster *extensionscontroller.Cluster, cloudProfileConfig *stackitv1alpha1.CloudProfileConfig) map[string]any {
	credentials, _ := vp.getCredentials(ctx, cp) // ignore missing credentials
	userAgentHeader := vp.getUserAgentHeaders(credentials, cluster)

	values := map[string]any{
		"enabled":                    getCSIDriver(cpConfig) == stackitv1alpha1.OPENSTACK,
		"rescanBlockStorageOnResize": cloudProfileConfig.RescanBlockStorageOnResize != nil && *cloudProfileConfig.RescanBlockStorageOnResize,
		"nodeVolumeAttachLimit":      cloudProfileConfig.NodeVolumeAttachLimit,
	}

	if userAgentHeader != nil {
		values["userAgentHeaders"] = userAgentHeader
	}

	return values
}

func (vp *valuesProvider) getControlPlaneShootChartCSISTACKITValues(ctx context.Context, cpConfig *stackitv1alpha1.ControlPlaneConfig, cp *extensionsv1alpha1.ControlPlane, cluster *extensionscontroller.Cluster, cloudProfileConfig *stackitv1alpha1.CloudProfileConfig) map[string]any {
	credentials, _ := vp.getCredentials(ctx, cp) // ignore missing credentials
	userAgentHeader := vp.getUserAgentHeaders(credentials, cluster)

	values := map[string]any{
		"enabled":                    getCSIDriver(cpConfig) == stackitv1alpha1.STACKIT,
		"rescanBlockStorageOnResize": cloudProfileConfig.RescanBlockStorageOnResize != nil && *cloudProfileConfig.RescanBlockStorageOnResize,
		"nodeVolumeAttachLimit":      cloudProfileConfig.NodeVolumeAttachLimit,
	}

	if userAgentHeader != nil {
		values["userAgentHeaders"] = userAgentHeader
	}

	return values
}

func (vp *valuesProvider) getAllWorkerPoolsZones(cluster *extensionscontroller.Cluster) []string {
	zones := sets.NewString()
	for _, worker := range cluster.Shoot.Spec.Provider.Workers {
		zones.Insert(worker.Zones...)
	}
	list := zones.UnsortedList()
	sort.Strings(list)
	return list
}

func cleanupSeedLegacyCSISnapshotValidation(ctx context.Context, client k8sclient.Client, namespace string) error {
	stackitSnapShotName := fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CSISnapshotValidationName)

	if err := kutil.DeleteObjects(
		ctx,
		client,
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: openstack.CSISnapshotValidationName, Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: openstack.CSISnapshotValidationName, Namespace: namespace}},
		&vpaautoscalingv1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "csi-snapshot-webhook-vpa", Namespace: namespace}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: openstack.CSISnapshotValidationName, Namespace: namespace}},
	); err != nil {
		return fmt.Errorf("failed to delete legacy csi-snapshot-validation resources: %w", err)
	}

	if err := kutil.DeleteObjects(
		ctx,
		client,
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: stackitSnapShotName, Namespace: namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: stackitSnapShotName, Namespace: namespace}},
		&vpaautoscalingv1.VerticalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("%s-csi-snapshot-webhook-vpa", CSIStackitPrefix), Namespace: namespace}},
		&policyv1.PodDisruptionBudget{ObjectMeta: metav1.ObjectMeta{Name: stackitSnapShotName, Namespace: namespace}},
	); err != nil {
		return fmt.Errorf("failed to delete legacy STACKIT snapshot-validation resources: %w", err)
	}

	return nil
}

func cleanupCloudProviderConfigSecret(ctx context.Context, client k8sclient.Client, namespace string) error {
	secretName := fmt.Sprintf("%s-%s", CSIStackitPrefix, openstack.CloudProviderConfigName)

	if err := kutil.DeleteObjects(
		ctx,
		client,
		&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace}},
	); err != nil {
		return fmt.Errorf("failed to delete legacy cloud-provider-config secret: %w", err)
	}

	return nil
}
