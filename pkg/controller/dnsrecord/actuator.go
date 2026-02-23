package dnsrecord

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	extensionscontroller "github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/dnsrecord"
	"github.com/gardener/gardener/extensions/pkg/util"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	extensionsv1alpha1helper "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1/helper"
	"github.com/go-logr/logr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/helper"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client"
)

type actuator struct {
	client client.Client

	dnsClientFunc dnsClientFunc
}

// NewActuator creates a new dnsrecord.Actuator.
func NewActuator(mgr manager.Manager) dnsrecord.Actuator {
	return &actuator{
		client:        mgr.GetClient(),
		dnsClientFunc: defaultDNSClientFunc(mgr.GetClient()),
	}
}

// Reconcile reconciles the DNSRecord.
func (a *actuator) Reconcile(ctx context.Context, log logr.Logger, dns *extensionsv1alpha1.DNSRecord, cluster *extensionscontroller.Cluster) error {
	dnsClient, err := a.dnsClientFunc(ctx, dns, cluster)
	if err != nil {
		return util.DetermineError(fmt.Errorf("could not create STACKIT client: %+v", err), helper.KnownCodes)
	}

	zoneID, err := getZone(ctx, log, dns, dnsClient)
	if err != nil {
		return err
	}

	ttl := extensionsv1alpha1helper.GetDNSRecordTTL(dns.Spec.TTL)

	log.Info("Creating or updating DNS recordset", "zone", zoneID, "name", dns.Spec.Name, "type", dns.Spec.RecordType, "values", dns.Spec.Values)
	if err := dnsClient.CreateOrUpdateRecordSet(ctx, zoneID, dns.Spec.Name, string(dns.Spec.RecordType), dns.Spec.Values, ttl); err != nil {
		if isZoneNotReadyError(err) {
			return gardencorev1beta1helper.NewErrorWithCodes(err, gardencorev1beta1.ErrorConfigurationProblem)
		}
		return err
	}

	if ptr.Deref(dns.Status.Zone, "") == zoneID {
		return nil
	}

	patch := client.MergeFrom(dns.DeepCopy())
	dns.Status.Zone = ptr.To(zoneID)
	return a.client.Status().Patch(ctx, dns, patch)
}

// Delete deletes the DNSRecord.
func (a *actuator) Delete(ctx context.Context, log logr.Logger, dns *extensionsv1alpha1.DNSRecord, cluster *extensionscontroller.Cluster) error {
	dnsClient, err := a.dnsClientFunc(ctx, dns, cluster)
	if err != nil {
		return util.DetermineError(fmt.Errorf("could not create STACKIT client: %+v", err), helper.KnownCodes)
	}

	zoneID, err := getZone(ctx, log, dns, dnsClient)
	if err != nil {
		return err
	}

	log.Info("Deleting DNS recordset", "zone", zoneID, "name", dns.Spec.Name, "type", dns.Spec.RecordType, "values", dns.Spec.Values)
	return stackitclient.IgnoreNotFoundError(dnsClient.DeleteRecordSet(ctx, zoneID, dns.Spec.Name, string(dns.Spec.RecordType)))
}

// Delete forcefully deletes the DNSRecord.
func (a *actuator) ForceDelete(ctx context.Context, log logr.Logger, dns *extensionsv1alpha1.DNSRecord, cluster *extensionscontroller.Cluster) error {
	return a.Delete(ctx, log, dns, cluster)
}

// Restore restores the DNSRecord.
func (a *actuator) Restore(ctx context.Context, log logr.Logger, dns *extensionsv1alpha1.DNSRecord, cluster *extensionscontroller.Cluster) error {
	return a.Reconcile(ctx, log, dns, cluster)
}

// Migrate migrates the DNSRecord.
func (a *actuator) Migrate(_ context.Context, _ logr.Logger, _ *extensionsv1alpha1.DNSRecord, _ *extensionscontroller.Cluster) error {
	return nil
}

type dnsClientFunc func(context.Context, *extensionsv1alpha1.DNSRecord, *extensionscontroller.Cluster) (stackitclient.DNSClient, error)

func defaultDNSClientFunc(c client.Client) dnsClientFunc {
	return func(ctx context.Context, dns *extensionsv1alpha1.DNSRecord, cluster *extensionscontroller.Cluster) (stackitclient.DNSClient, error) {
		// DNS is a global endpoint, so we don't need to specify a region
		return stackitclient.New("", cluster).DNS(ctx, c, dns.Spec.SecretRef)
	}
}

// getZone retrives the zoneID that the record needs to be created in.
// In accordance with https://gardener.cloud/docs/gardener/extensions/resources/dnsrecord/#what-needs-to-be-implemented-to-support-a-new-dns-provider
// we first check the spec, then the status (where we persist the ID), and finally list all zones to find the matching one.
func getZone(ctx context.Context, log logr.Logger, dns *extensionsv1alpha1.DNSRecord, dnsClient stackitclient.DNSClient) (string, error) {
	switch {
	case ptr.Deref(dns.Spec.Zone, "") != "":
		return *dns.Spec.Zone, nil
	case ptr.Deref(dns.Status.Zone, "") != "":
		return *dns.Status.Zone, nil
	default:
		stackitZones, err := dnsClient.ListZones(ctx)
		if err != nil {
			return "", err
		}
		log.Info("got zones from STACKIT API", "zones", stackitZones)
		zones := make(map[string]string, len(stackitZones))
		for _, zone := range stackitZones {
			zones[zone.DNSName] = zone.ID
		}
		zoneID := dnsrecord.FindZoneForName(zones, dns.Spec.Name)
		if zoneID == "" {
			return "", gardencorev1beta1helper.NewErrorWithCodes(fmt.Errorf("could not find DNS hosted zone for name %s", dns.Spec.Name), gardencorev1beta1.ErrorConfigurationProblem)
		}
		return zoneID, nil
	}
}

func isZoneNotReadyError(err error) bool {
	var stackitErr *stackitclient.Error
	if !errors.As(err, &stackitErr) {
		return false
	}
	return stackitErr.StatusCode == http.StatusBadRequest && strings.HasPrefix(stackitErr.Message, "zone is not ready")
}
