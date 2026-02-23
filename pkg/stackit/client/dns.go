package client

import (
	"context"
	"fmt"
	"strings"

	sdkconfig "github.com/stackitcloud/stackit-sdk-go/core/config"
	"github.com/stackitcloud/stackit-sdk-go/services/dns"
	"k8s.io/utils/ptr"
	"k8s.io/utils/set"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

func NewDNSClient(ctx context.Context, endpoints stackitv1alpha1.APIEndpoints, credentials *stackit.Credentials) (DNSClient, error) {
	options := clientOptions(nil, endpoints, credentials)

	if endpoints.DNS != nil {
		options = append(options, sdkconfig.WithEndpoint(*endpoints.DNS))
	}

	apiClient, err := dns.NewAPIClient(options...)
	if err != nil {
		return nil, err
	}
	return &dnsClient{
		api:       apiClient,
		projectID: credentials.ProjectID,
	}, nil
}

type DNSClient interface {
	ListZones(ctx context.Context) ([]DNSZone, error)
	CreateOrUpdateRecordSet(ctx context.Context, zoneID, name, recordType string, records []string, ttl int64) error
	DeleteRecordSet(ctx context.Context, zoneID, name, recordType string) error
}

type DNSZone struct {
	ID      string
	DNSName string
}

type dnsClient struct {
	api dns.DefaultApi

	projectID string
}

func (c *dnsClient) ListZones(ctx context.Context) ([]DNSZone, error) {
	dnsZonesResp, err := c.api.ListZonesExecute(ctx, c.projectID)
	if err != nil {
		return nil, err
	}

	if dnsZonesResp == nil || dnsZonesResp.Zones == nil {
		return []DNSZone{}, nil
	}

	result := make([]DNSZone, 0, len(*dnsZonesResp.Zones))
	for _, zone := range *dnsZonesResp.Zones {
		result = append(result, DNSZone{
			ID:      zone.GetId(),
			DNSName: zone.GetDnsName(),
		})
	}

	return result, nil
}

func (c *dnsClient) CreateOrUpdateRecordSet(ctx context.Context,
	zoneID, name, recordType string, wantedRecords []string, ttl int64,
) error {
	recordSet, err := c.findRecordSet(ctx, zoneID, name, recordType)
	if err != nil {
		return fmt.Errorf("failed to find record set: %w", err)
	}

	wantedRecordsPayload := []dns.RecordPayload{}
	for _, record := range wantedRecords {
		wantedRecordsPayload = append(wantedRecordsPayload, dns.RecordPayload{
			Content: ptr.To(record),
		})
	}

	if recordSet == nil {
		_, err := c.api.CreateRecordSet(ctx, c.projectID, zoneID).CreateRecordSetPayload(dns.CreateRecordSetPayload{
			Name:    &name,
			Records: &wantedRecordsPayload,
			Type:    ptr.To(dns.CreateRecordSetPayloadTypes(recordType)),
			Ttl:     ptr.To(ttl),
		}).Execute()
		if err != nil {
			return fmt.Errorf("failed to create record set: %w", err)
		}
		return nil
	}

	if recordSet.GetTtl() == ttl && areRecordsEqual(recordSet.GetRecords(), wantedRecords) {
		// If TTL and records are the same, no update is necessary
		return nil
	}

	_, err = c.api.PartialUpdateRecordSet(ctx, c.projectID, zoneID, recordSet.GetId()).PartialUpdateRecordSetPayload(dns.PartialUpdateRecordSetPayload{
		Name:    &name,
		Records: &wantedRecordsPayload,
		Ttl:     ptr.To(ttl),
	}).Execute()
	if err != nil {
		return fmt.Errorf("failed to update record set: %w", err)
	}

	return nil
}

func (c *dnsClient) DeleteRecordSet(ctx context.Context, zoneID, name, recordType string) error {
	recordSet, err := c.findRecordSet(ctx, zoneID, name, recordType)
	if err != nil {
		return fmt.Errorf("failed to find record set: %w", err)
	}
	if recordSet == nil {
		return nil
	}

	_, err = c.api.DeleteRecordSetExecute(ctx, c.projectID, zoneID, recordSet.GetId())
	if err != nil {
		return fmt.Errorf("failed to delete record set: %w", err)
	}
	return nil
}

func (c *dnsClient) findRecordSet(ctx context.Context, zoneID, name, recordType string) (*dns.RecordSet, error) {
	resp, err := c.api.ListRecordSetsExecute(ctx, c.projectID, zoneID)
	if err != nil {
		return nil, err
	}
	// in case either name is a FQDN we remove the trailing dot
	name = strings.TrimSuffix(name, ".")
	for _, recordSet := range resp.GetRrSets() {
		if !recordSet.GetActive() {
			continue
		}
		if strings.TrimSuffix(recordSet.GetName(), ".") != name {
			continue
		}
		if string(recordSet.GetType()) != recordType {
			continue
		}
		return &recordSet, nil
	}
	return nil, nil
}

func areRecordsEqual(existingRecords []dns.Record, newRecords []string) bool {
	if len(existingRecords) != len(newRecords) {
		return false
	}

	existingRecordsSet := set.New[string]()
	for _, record := range existingRecords {
		existingRecordsSet.Insert(record.GetContent())
	}

	return existingRecordsSet.Equal(set.New(newRecords...))
}
