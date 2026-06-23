package controlplane

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gardener/gardener/pkg/chartrenderer"
	gardenerutils "github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/managedresources"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/charts"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/imagevector"
	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/openstack"
)

func NewCompatCSICompatibilityHandler(client client.Client, config *rest.Config) *CompatCSICompatibilityHandler {
	return &CompatCSICompatibilityHandler{
		client: client,
		config: config,
	}
}

type CompatCSICompatibilityHandler struct {
	client client.Client
	config *rest.Config
}

func (ch *CompatCSICompatibilityHandler) HandleSeedCSICompatibility(ctx context.Context, namespace string, cpConfig *stackitv1alpha1.ControlPlaneConfig, controlPlaneValues map[string]any) error {
	if getCSICompatibilityMode(cpConfig) != stackitv1alpha1.DEFAULT {
		chart, err := ch.renderSeedCSICompatibilityMode(controlPlaneValues)
		if err != nil {
			return fmt.Errorf("failed to render seed CSI compatibility mode: %w", err)
		}
		err = ch.deploySeedCSICompatibilityMode(ctx, namespace, chart)
		if err != nil {
			return fmt.Errorf("failed to deploy seed CSI compatibility mode: %w", err)
		}
	} else {
		err := ch.deleteSeedCSICompatibilityMode(ctx, namespace)
		if err != nil {
			return fmt.Errorf("failed to deploy seed CSI compatibility mode: %w", err)
		}
	}
	return nil
}

func (ch *CompatCSICompatibilityHandler) renderSeedCSICompatibilityMode(values map[string]any) (*chartrenderer.RenderedChart, error) {
	renderer, err := chartrenderer.NewForConfig(ch.config)
	if err != nil {
		return nil, err
	}

	// TODO: constant
	chartName := "stackit-blockstorage-csi-driver"

	// Get the chart Values
	csiStackitValues := values[openstack.CSISTACKITControllerName].(map[string]any)
	// Merge csiStackitValues to topLevel. Basically removes the openstack.CSISTACKITControllerName key
	chartValues := gardenerutils.MergeMaps(values, csiStackitValues)
	// Override chart values
	chartValues["prefix"] = "stackit-compat"

	//TODO: Use gardener tools for this? If possible
	imagesToFind := []string{
		"csi-driver-stackit",
		"csi-provisioner",
		"csi-attacher",
		"csi-snapshotter",
		"csi-resizer",
		"csi-liveness-probe",
		"csi-snapshot-controller",
	}
	images := imagevector.ImageVector()
	imageMap := make(map[string]any)

	for _, image := range imagesToFind {
		foundImage, err := images.FindImage(image)
		if err != nil {
			return nil, err
		}
		imageMap[image] = foundImage.String()
	}
	chartValues["images"] = imageMap

	return renderer.RenderEmbeddedFS(
		charts.InternalChart,
		filepath.Join(charts.InternalChartsPath, "seed-controlplane/charts/stackit-blockstorage-csi-driver"),
		chartName,
		"kube-system",
		chartValues,
	)
}

func (ch *CompatCSICompatibilityHandler) deploySeedCSICompatibilityMode(ctx context.Context, namespace string, renderedChart *chartrenderer.RenderedChart) error {
	data := renderedChart.AsSecretData()
	return managedresources.CreateForSeed(ctx, ch.client, namespace, "stackit-csi-compat-chart", false, data)
}

func (ch *CompatCSICompatibilityHandler) deleteSeedCSICompatibilityMode(ctx context.Context, namespace string) error {
	return managedresources.DeleteForSeed(ctx, ch.client, namespace, "stackit-csi-compat-chart")
}

func (ch *CompatCSICompatibilityHandler) HandleShootCSICompatibility(ctx context.Context, namespace string, cpConfig *stackitv1alpha1.ControlPlaneConfig, values map[string]any) error {
	compatibilityMode := getCSICompatibilityMode(cpConfig)
	if compatibilityMode != stackitv1alpha1.DEFAULT {
		blockLegacyCreation := compatibilityMode == stackitv1alpha1.COMPATBLOCK
		chart, err := ch.renderShootCSICompatibilityMode(values, blockLegacyCreation)
		if err != nil {
			return fmt.Errorf("render shoot CSI compatibility mode: %w", err)
		}
		err = ch.deployShootCSICompatibilityMode(ctx, namespace, chart)
		if err != nil {
			return fmt.Errorf("deploy shoot CSI compatibility mode: %w", err)
		}
	} else {
		err := ch.deleteShootCSICompatibilityMode(ctx, namespace)
		if err != nil {
			return fmt.Errorf("delete shoot CSI compatibility mode: %w", err)
		}
	}
	return nil
}

func (ch *CompatCSICompatibilityHandler) renderShootCSICompatibilityMode(values map[string]any, blockLegacyCreation bool) (*chartrenderer.RenderedChart, error) {
	renderer, err := chartrenderer.NewForConfig(ch.config)
	if err != nil {
		return nil, err
	}

	// TODO: constant
	chartName := "stackit-blockstorage-csi-driver"

	// Get the chart Values
	csiStackitValues := values[openstack.CSISTACKITControllerName].(map[string]any)
	// Merge csiStackitValues to topLevel. Basically removes the openstack.CSISTACKITControllerName key
	chartValues := gardenerutils.MergeMaps(values, csiStackitValues)
	// Override chart values
	chartValues["prefix"] = "stackit-compat"

	//TODO: Use gardener tools for this? If possible
	imagesToFind := []string{
		"csi-driver-stackit",
		"csi-node-driver-registrar",
		"csi-liveness-probe",
	}
	images := imagevector.ImageVector()
	imageMap := make(map[string]any)

	for _, image := range imagesToFind {
		foundImage, err := images.FindImage(image)
		if err != nil {
			return nil, err
		}
		imageMap[image] = foundImage.String()
	}
	chartValues["images"] = imageMap
	chartValues["healthzPort"] = 9909
	csiValues := map[string]any{
		"enableCompatibilityMode": true,
	}
	if blockLegacyCreation {
		csiValues["blockLegacyCreation"] = true
	}
	chartValues["csi"] = csiValues

	return renderer.RenderEmbeddedFS(
		charts.InternalChart,
		filepath.Join(charts.InternalChartsPath, "shoot-system-components/charts/stackit-blockstorage-csi-driver"),
		chartName,
		"kube-system",
		chartValues,
	)
}

func (ch *CompatCSICompatibilityHandler) deployShootCSICompatibilityMode(ctx context.Context, namespace string, renderedChart *chartrenderer.RenderedChart) error {
	data := renderedChart.AsSecretData()
	return managedresources.CreateForShoot(ctx, ch.client, namespace, "stackit-csi-compat-shoot-chart", "gardener-extension-provider-stackit", false, data)
}

func (ch *CompatCSICompatibilityHandler) deleteShootCSICompatibilityMode(ctx context.Context, namespace string) error {
	return managedresources.DeleteForShoot(ctx, ch.client, namespace, "stackit-csi-compat-shoot-chart")
}
