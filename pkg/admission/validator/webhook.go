package validator

import (
	extensionswebhook "github.com/gardener/gardener/extensions/pkg/webhook"
	"github.com/gardener/gardener/pkg/apis/core"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
)

const (
	// Name is a name for a validation webhook.
	Name = "validator"
)

var logger = log.Log.WithName("stackit-validator-webhook")

// New creates a new webhook that validates Shoot, CloudProfile, NamespacedCloudProfile, SecretBinding and CredentialsBinding resources.
func New(mgr manager.Manager) (*extensionswebhook.Webhook, error) {
	logger.Info("Setting up webhook", "name", Name)

	return extensionswebhook.New(mgr, extensionswebhook.Args{
		Provider: stackit.Type,
		Name:     Name,
		Path:     "/webhooks/validate",
		Validators: map[extensionswebhook.Validator][]extensionswebhook.Type{
			NewCloudProfileValidator(mgr):           {{Obj: &core.CloudProfile{}}},
			NewNamespacedCloudProfileValidator(mgr): {{Obj: &core.NamespacedCloudProfile{}}},
		},
		Target: extensionswebhook.TargetSeed,
		ObjectSelector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"provider.extensions.gardener.cloud/" + stackit.Type: "true"},
		},
	})
}
