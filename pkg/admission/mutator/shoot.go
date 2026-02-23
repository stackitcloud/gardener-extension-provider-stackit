package mutator

import (
	"bytes"
	"context"
	"fmt"
	"reflect"

	configv1alpha1 "github.com/gardener/gardener-extension-os-coreos/pkg/controller/config/v1alpha1"
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	"golang.org/x/mod/semver"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/feature"
)

// FlatcarImageVersion is the OEM image that supports PTP.
var FlatcarImageVersion string

type shoot struct {
	decoder runtime.Decoder
}

var (
	scheme  = runtime.NewScheme()
	encoder runtime.Encoder
)

func init() {
	utilruntime.Must(configv1alpha1.AddToScheme(scheme))
	encoder = serializer.NewCodecFactory(scheme).EncoderForVersion(&json.Serializer{}, configv1alpha1.SchemeGroupVersion)
}

// NewShootMutator returns a new instance of a shoot mutator.
func NewShootMutator(mgr manager.Manager) extensionswebhook.Mutator {
	logger.Info("MutateDisableNTP", "enabled", feature.Gate.Enabled(feature.MutateDisableNTP))
	logger.Info("FlatcarImageVersion", "version", FlatcarImageVersion)

	return &shoot{
		decoder: serializer.NewCodecFactory(mgr.GetScheme(), serializer.EnableStrict).UniversalDecoder(),
	}
}

func (s *shoot) Mutate(ctx context.Context, newObj, oldObj client.Object) error {
	shoot, ok := newObj.(*gardencorev1beta1.Shoot)
	if !ok {
		return fmt.Errorf("wrong object type: %T", newObj)
	}

	// Skip if shoot is in restore or migration phase
	if wasShootRescheduledToNewSeed(shoot) {
		return nil
	}

	var oldShoot *gardencorev1beta1.Shoot
	if oldObj != nil {
		oldShoot, ok = oldObj.(*gardencorev1beta1.Shoot)
		if !ok {
			return fmt.Errorf("wrong object type %T", oldObj)
		}
	}

	// This check is only relevant for UPDATE operations (when oldShoot is not nil).
	// If oldShoot is nil (CREATE operation), this entire 'if' block is correctly skipped,
	// allowing the mutation to always apply on creation.
	if oldShoot != nil && reflect.DeepEqual(shoot.Spec, oldShoot.Spec) {
		return nil
	}

	if oldShoot != nil && isShootInMigrationOrRestorePhase(shoot) {
		return nil
	}

	// Skip if shoot is in deletion phase
	if shoot.DeletionTimestamp != nil || oldShoot != nil && oldShoot.DeletionTimestamp != nil {
		return nil
	}

	// Skip if it's a workerless Shoot
	if gardencorev1beta1helper.IsWorkerless(shoot) {
		return nil
	}

	if feature.Gate.Enabled(feature.MutateDisableNTP) {
		// Check and update machine image versions
		if err := s.mutateMachineImageVersion(shoot); err != nil {
			return fmt.Errorf("failed to mutate machine image version: %w", err)
		}
	}

	return nil
}

// wasShootRescheduledToNewSeed returns true if the shoot.Spec.SeedName has been changed, but the migration operation has not started yet.
func wasShootRescheduledToNewSeed(shoot *gardencorev1beta1.Shoot) bool {
	return shoot.Status.LastOperation != nil &&
		shoot.Status.LastOperation.Type != gardencorev1beta1.LastOperationTypeMigrate &&
		shoot.Spec.SeedName != nil &&
		shoot.Status.SeedName != nil &&
		*shoot.Spec.SeedName != *shoot.Status.SeedName
}

// isShootInMigrationOrRestorePhase returns true if the shoot is currently being migrated or restored.
func isShootInMigrationOrRestorePhase(shoot *gardencorev1beta1.Shoot) bool {
	return shoot.Status.LastOperation != nil &&
		(shoot.Status.LastOperation.Type == gardencorev1beta1.LastOperationTypeRestore &&
			shoot.Status.LastOperation.State != gardencorev1beta1.LastOperationStateSucceeded ||
			shoot.Status.LastOperation.Type == gardencorev1beta1.LastOperationTypeMigrate)
}

// mutateMachineImageVersion checks if any worker's Flatcar image version is greater than or equal to FlatcarImageVersion
// and disables the ntp service.
func (s *shoot) mutateMachineImageVersion(shoot *gardencorev1beta1.Shoot) error {
	ptpOverride := configv1alpha1.ExtensionConfig{NTP: &configv1alpha1.NTPConfig{
		Enabled: ptr.To(false),
	}}
	providerConfigBuf := new(bytes.Buffer)
	err := encoder.Encode(&ptpOverride, providerConfigBuf)
	if err != nil {
		return err
	}

	for i, worker := range shoot.Spec.Provider.Workers {
		if worker.Machine.Image != nil && worker.Machine.Image.Name == "coreos" {
			currentVersion := "v" + *worker.Machine.Image.Version
			targetVersion := "v" + FlatcarImageVersion

			if semver.Compare(currentVersion, targetVersion) >= 0 {
				if worker.Machine.Image.ProviderConfig != nil {
					var existingConfig configv1alpha1.ExtensionConfig
					if _, _, err := s.decoder.Decode(worker.Machine.Image.ProviderConfig.Raw, nil, &existingConfig); err != nil {
						return fmt.Errorf("failed to decode existing provider config for worker pool %s: %w", worker.Name, err)
					}

					// Check if NTP is already disabled
					// if disabled skip the worker mutate
					if existingConfig.NTP != nil && existingConfig.NTP.Enabled != nil && !*existingConfig.NTP.Enabled {
						continue
					}
				}

				shoot.Spec.Provider.Workers[i].Machine.Image.ProviderConfig = &runtime.RawExtension{Raw: providerConfigBuf.Bytes()}
				logger.Info("PTP was enabled",
					"namespace", shoot.Namespace, "shoot", shoot.Name, "node-pool", worker.Name)
			}
		}
	}
	return nil
}
