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

const (
	csiDriverChartName      = "stackit-blockstorage-csi-driver"
	csiCompatibilityPrefix  = "stackit-csi-compat"
	csiCompatSeedChartName  = csiCompatibilityPrefix + "-chart"
	csiCompatShootChartName = csiCompatibilityPrefix + "-shoort-chart"
)

func NewCompatCSICompatibilityHandler(client client.Client, config *rest.Config) (*CompatCSICompatibilityHandler, error) {
	renderer, err := chartrenderer.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return &CompatCSICompatibilityHandler{
		client:   client,
		renderer: renderer,
	}, nil
}

type CompatCSICompatibilityHandler struct {
	client   client.Client
	renderer chartrenderer.Interface
}

func (ch *CompatCSICompatibilityHandler) HandleSeedCSICompatibility(ctx context.Context, namespace string, cpConfig *stackitv1alpha1.ControlPlaneConfig, controlPlaneValues map[string]any) error {
	compatibilityMode := getCSICompatibilityMode(cpConfig)
	switch compatibilityMode {
	case stackitv1alpha1.COMPAT, stackitv1alpha1.COMPATBLOCK:
		blockLegacyCreation := compatibilityMode == stackitv1alpha1.COMPATBLOCK
		chart, err := ch.renderSeedCSICompatibilityMode(controlPlaneValues, blockLegacyCreation)
		if err != nil {
			return fmt.Errorf("failed to render seed CSI compatibility mode: %w", err)
		}
		err = ch.deploySeedCSICompatibilityMode(ctx, namespace, chart)
		if err != nil {
			return fmt.Errorf("failed to deploy seed CSI compatibility mode: %w", err)
		}
	default:
		err := ch.deleteSeedCSICompatibilityMode(ctx, namespace)
		if err != nil {
			return fmt.Errorf("failed to deploy seed CSI compatibility mode: %w", err)
		}
	}
	return nil
}

func (ch *CompatCSICompatibilityHandler) renderSeedCSICompatibilityMode(values map[string]any, blockLegacyCreation bool) (*chartrenderer.RenderedChart, error) {
	chartValues := composeCompatibilityChartValues(values)

	// Override chart values
	chartValues["prefix"] = csiCompatibilityPrefix

	imageMap, err := findImages(
		"csi-driver-stackit",
		"csi-provisioner",
		"csi-attacher",
		"csi-snapshotter",
		"csi-resizer",
		"csi-liveness-probe",
		"csi-snapshot-controller",
	)
	if err != nil {
		return nil, err
	}
	chartValues["images"] = imageMap

	csiValues := map[string]any{
		"enableCompatibilityMode": true,
	}
	if blockLegacyCreation {
		csiValues["blockLegacyCreation"] = true
	}
	chartValues["csi"] = csiValues

	return ch.renderer.RenderEmbeddedFS(
		charts.InternalChart,
		filepath.Join(charts.InternalChartsPath, "seed-controlplane/charts/stackit-blockstorage-csi-driver"),
		csiDriverChartName,
		"kube-system",
		chartValues,
	)
}

func (ch *CompatCSICompatibilityHandler) deploySeedCSICompatibilityMode(ctx context.Context, namespace string, renderedChart *chartrenderer.RenderedChart) error {
	data := renderedChart.AsSecretData()
	return managedresources.CreateForSeed(ctx, ch.client, namespace, csiCompatSeedChartName, false, data)
}

func (ch *CompatCSICompatibilityHandler) deleteSeedCSICompatibilityMode(ctx context.Context, namespace string) error {
	return client.IgnoreNotFound(managedresources.DeleteForSeed(ctx, ch.client, namespace, csiCompatSeedChartName))
}

func (ch *CompatCSICompatibilityHandler) HandleShootCSICompatibility(ctx context.Context, namespace string, cpConfig *stackitv1alpha1.ControlPlaneConfig, values map[string]any) error {
	compatibilityMode := getCSICompatibilityMode(cpConfig)
	switch compatibilityMode {
	case stackitv1alpha1.COMPAT, stackitv1alpha1.COMPATBLOCK:
		chart, err := ch.renderShootCSICompatibilityMode(values)
		if err != nil {
			return fmt.Errorf("render shoot CSI compatibility mode: %w", err)
		}
		err = ch.deployShootCSICompatibilityMode(ctx, namespace, chart)
		if err != nil {
			return fmt.Errorf("deploy shoot CSI compatibility mode: %w", err)
		}
	default:
		err := ch.deleteShootCSICompatibilityMode(ctx, namespace)
		if err != nil {
			return fmt.Errorf("delete shoot CSI compatibility mode: %w", err)
		}
	}
	return nil
}

func (ch *CompatCSICompatibilityHandler) renderShootCSICompatibilityMode(values map[string]any) (*chartrenderer.RenderedChart, error) {
	chartValues := composeCompatibilityChartValues(values)

	// Override chart values
	chartValues["prefix"] = csiCompatibilityPrefix

	imageMap, err := findImages(
		"csi-driver-stackit",
		"csi-node-driver-registrar",
		"csi-liveness-probe",
	)
	if err != nil {
		return nil, err
	}
	chartValues["images"] = imageMap

	chartValues["healthzPort"] = 9909
	csiValues := map[string]any{
		"enableCompatibilityMode": true,
	}
	chartValues["csi"] = csiValues

	return ch.renderer.RenderEmbeddedFS(
		charts.InternalChart,
		filepath.Join(charts.InternalChartsPath, "shoot-system-components/charts/stackit-blockstorage-csi-driver"),
		csiDriverChartName,
		"kube-system",
		chartValues,
	)
}

func (ch *CompatCSICompatibilityHandler) deployShootCSICompatibilityMode(ctx context.Context, namespace string, renderedChart *chartrenderer.RenderedChart) error {
	data := renderedChart.AsSecretData()
	return managedresources.CreateForShoot(ctx, ch.client, namespace, csiCompatShootChartName, "gardener-extension-provider-stackit", false, data)
}

func (ch *CompatCSICompatibilityHandler) deleteShootCSICompatibilityMode(ctx context.Context, namespace string) error {
	return client.IgnoreNotFound(managedresources.DeleteForShoot(ctx, ch.client, namespace, csiCompatShootChartName))
}

// composeCompatibilityChartValues returns a copy of the given values map merged with the csiStackitValues on topLevel.
// Basically removes the openstack.CSISTACKITControllerName key
func composeCompatibilityChartValues(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	csiStackitValues, ok := values[openstack.CSISTACKITControllerName].(map[string]any)
	if !ok {
		csiStackitValues = nil
	}
	return gardenerutils.MergeMaps(values, csiStackitValues)
}

func findImages(imagesToFind ...string) (map[string]any, error) {
	images := imagevector.ImageVector()
	result := make(map[string]any)
	for _, image := range imagesToFind {
		foundImage, err := images.FindImage(image)
		if err != nil {
			return nil, err
		}
		result[image] = foundImage.String()
	}
	return result, nil
}
