package client

import (
	"context"
	"math/rand/v2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	dns "github.com/stackitcloud/stackit-sdk-go/services/dns/v1api"
	"go.uber.org/mock/gomock"

	mock "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit/client/mock/dns"
)

var _ = Describe("DNSClient", func() {
	var (
		ctx    context.Context
		client *dnsClient

		mockAPI *mock.MockDefaultAPI
		ctrl    *gomock.Controller
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockAPI = mock.NewMockDefaultAPI(ctrl)
		client = &dnsClient{
			api:       mockAPI,
			projectID: "test-project",
		}
	})

	Describe("List Zones", func() {
		It("should get the list of DNS zones", func() {
			expectedZones := []DNSZone{
				{ID: "zone1", DNSName: "example.com."},
				{ID: "zone2", DNSName: "example.org."},
			}
			response := dns.ListZonesResponse{
				Zones: []dns.Zone{
					{Id: "zone1", DnsName: "example.com."},
					{Id: "zone2", DnsName: "example.org."},
				},
			}
			mockAPI.EXPECT().ListZones(ctx, client.projectID).Return(dns.ApiListZonesRequest{ApiService: mockAPI})
			mockAPI.EXPECT().ListZonesExecute(gomock.Any()).Return(&response, nil)
			actualZones, err := client.ListZones(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(actualZones).To(Equal(expectedZones))
		})
	})

	Describe("CreateOrUpdate Record", func() {
		BeforeEach(func() {
			mockAPI.EXPECT().ListRecordSets(ctx, client.projectID, "zone1").Return(dns.ApiListRecordSetsRequest{ApiService: mockAPI})
			mockAPI.EXPECT().ListRecordSetsExecute(gomock.Any()).Return(&dns.ListRecordSetsResponse{
				RrSets: []dns.RecordSet{
					{
						Name:    "test.example.com.",
						Active:  new(true),
						Type:    "A",
						Records: []dns.Record{{Content: "1.1.1.1"}},
						Id:      "some-uuid",
						Ttl:     300,
					},
					{
						Name:    "test.example.com.",
						Active:  new(false),
						Type:    "A",
						Records: []dns.Record{{Content: "4.4.4.4"}},
						Id:      "some-uuid2",
						Ttl:     300,
					},
				},
			}, nil)
		})

		It("should create a new record set if it does not exist", func() {
			mockAPI.EXPECT().CreateRecordSet(ctx, client.projectID, "zone1").Return(dns.ApiCreateRecordSetRequest{ApiService: mockAPI})
			mockAPI.EXPECT().CreateRecordSetExecute(gomock.Any()).Return(nil, nil)

			Expect(client.CreateOrUpdateRecordSet(ctx, "zone1", "new.example.com.", "A", []string{"1.1.1.1"}, 300)).To(Succeed())
		})

		It("should update the existing record set if it exists and records are different", func() {
			mockAPI.EXPECT().PartialUpdateRecordSet(ctx, client.projectID, "zone1", "some-uuid").Return(dns.ApiPartialUpdateRecordSetRequest{ApiService: mockAPI})
			mockAPI.EXPECT().PartialUpdateRecordSetExecute(gomock.Any()).Return(nil, nil)

			Expect(client.CreateOrUpdateRecordSet(ctx, "zone1", "test.example.com.", "A", []string{"4.4.4.4"}, 300)).To(Succeed())
		})

		It("should do nothing if the existing record set has the same records and TTL", func() {
			Expect(client.CreateOrUpdateRecordSet(ctx, "zone1", "test.example.com.", "A", []string{"1.1.1.1"}, 300)).To(Succeed())
		})
	})

	Describe("Delete Record", func() {
		BeforeEach(func() {
			mockAPI.EXPECT().ListRecordSets(ctx, client.projectID, "zone1").Return(dns.ApiListRecordSetsRequest{ApiService: mockAPI})
			mockAPI.EXPECT().ListRecordSetsExecute(gomock.Any()).Return(&dns.ListRecordSetsResponse{
				RrSets: []dns.RecordSet{{
					Name:   "test.example.com.",
					Active: new(true),
					Type:   "A",
					Id:     "some-uuid",
				}},
			}, nil)
		})

		It("should do nothing if the record set does not exist", func() {
			Expect(client.DeleteRecordSet(ctx, "zone1", "nonexistent.example.com.", "A")).To(Succeed())
		})

		It("should delete the record set if it exists", func() {
			mockAPI.EXPECT().DeleteRecordSet(ctx, client.projectID, "zone1", "some-uuid").Return(dns.ApiDeleteRecordSetRequest{ApiService: mockAPI})
			mockAPI.EXPECT().DeleteRecordSetExecute(gomock.Any()).Return(nil, nil)

			Expect(client.DeleteRecordSet(ctx, "zone1", "test.example.com.", "A")).To(Succeed())
		})

		It("should delete the record even if a non-FQDN is specified", func() {
			mockAPI.EXPECT().DeleteRecordSet(ctx, client.projectID, "zone1", "some-uuid").Return(dns.ApiDeleteRecordSetRequest{ApiService: mockAPI})
			mockAPI.EXPECT().DeleteRecordSetExecute(gomock.Any()).Return(nil, nil)

			Expect(client.DeleteRecordSet(ctx, "zone1", "test.example.com", "A")).To(Succeed())
		})
	})

	Describe("findRecordSet", func() {
		BeforeEach(func() {
			rrSets := []dns.RecordSet{
				{
					Name:    "active.example.com.",
					Active:  new(true),
					Type:    "A",
					Records: []dns.Record{{Content: "1.1.1.1"}},
					Id:      "active-a-uuid",
				},
				{
					Name:    "active2.example.com.",
					Active:  new(true),
					Type:    "A",
					Records: []dns.Record{{Content: "1.1.1.1"}},
					Id:      "active2-a-uuid",
				},
				{
					Name:    "active.example.com.",
					Active:  new(true),
					Type:    "TXT",
					Records: []dns.Record{{Content: "hello-world"}},
					Id:      "active-txt-uuid",
				},
				{
					Name:    "inactive.example.com.",
					Active:  new(false),
					Type:    "A",
					Records: []dns.Record{{Content: "2.2.2.2"}},
					Id:      "inactive-a-uuid",
				},
			}
			rand.Shuffle(len(rrSets), func(i, j int) {
				rrSets[i], rrSets[j] = rrSets[j], rrSets[i]
			})
			mockAPI.EXPECT().ListRecordSets(ctx, client.projectID, "zone1").Return(dns.ApiListRecordSetsRequest{ApiService: mockAPI})
			mockAPI.EXPECT().ListRecordSetsExecute(gomock.Any()).Return(&dns.ListRecordSetsResponse{
				RrSets: rrSets,
			}, nil)
		})

		It("should return the correct A recordSet", func() {
			recordSet, err := client.findRecordSet(ctx, "zone1", "active.example.com.", "A")
			Expect(err).ToNot(HaveOccurred())
			Expect(recordSet).ToNot(BeNil())
			Expect(recordSet.GetId()).To(Equal("active-a-uuid"))
		})

		It("should return the correct TXT recordSet", func() {
			recordSet, err := client.findRecordSet(ctx, "zone1", "active.example.com.", "TXT")
			Expect(err).ToNot(HaveOccurred())
			Expect(recordSet).ToNot(BeNil())
			Expect(recordSet.GetId()).To(Equal("active-txt-uuid"))
		})

		It("should return nil if nothing matches", func() {
			recordSet, err := client.findRecordSet(ctx, "zone1", "non-existant.example.com.", "A")
			Expect(err).ToNot(HaveOccurred())
			Expect(recordSet).To(BeNil())
		})
	})
})

var _ = DescribeTable("areRecordsEqual",
	func(existingRecords []dns.Record, newRecords []string, expected bool) {
		Expect(areRecordsEqual(existingRecords, newRecords)).To(Equal(expected))
	},
	Entry("equal records",
		[]dns.Record{{Content: "1.2.3.4"}},
		[]string{"1.2.3.4"},
		true,
	),
	Entry("equal records, different order",
		[]dns.Record{{Content: "1.2.3.4"}, {Content: "5.6.7.8"}},
		[]string{"5.6.7.8", "1.2.3.4"},
		true,
	),
	Entry("different records",
		[]dns.Record{{Content: "1.2.3.4"}},
		[]string{"5.6.7.8"},
		false,
	),
	Entry("subset records",
		[]dns.Record{{Content: "1.2.3.4"}},
		[]string{"1.2.3.4", "5.6.7.8"},
		false,
	),
)
