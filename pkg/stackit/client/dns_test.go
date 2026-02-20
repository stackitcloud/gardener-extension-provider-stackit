package client

import (
	"context"
	"math/rand/v2"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/stackitcloud/stackit-sdk-go/services/dns"
	"go.uber.org/mock/gomock"
	"k8s.io/utils/ptr"

	mock "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client/mock/dns"
)

var _ = Describe("DNSClient", func() {
	var (
		ctx    context.Context
		client *dnsClient

		mockAPI *mock.MockDefaultApi
		ctrl    *gomock.Controller
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockAPI = mock.NewMockDefaultApi(ctrl)
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
				Zones: &[]dns.Zone{
					{Id: ptr.To("zone1"), DnsName: ptr.To("example.com.")},
					{Id: ptr.To("zone2"), DnsName: ptr.To("example.org.")},
				},
			}
			mockAPI.EXPECT().ListZonesExecute(ctx, client.projectID).Return(&response, nil)
			actualZones, err := client.ListZones(ctx)
			Expect(err).ToNot(HaveOccurred())
			Expect(actualZones).To(Equal(expectedZones))
		})
	})

	Describe("CreateOrUpdate Record", func() {
		var (
			mockCreateRequest *mock.MockApiCreateRecordSetRequest
			mockUpdateRequest *mock.MockApiPartialUpdateRecordSetRequest
		)
		BeforeEach(func() {
			mockAPI.EXPECT().ListRecordSetsExecute(ctx, client.projectID, "zone1").Return(&dns.ListRecordSetsResponse{
				RrSets: &[]dns.RecordSet{
					{
						Name:    ptr.To("test.example.com."),
						Active:  ptr.To(true),
						Type:    dns.RecordSetGetTypeAttributeType(ptr.To("A")),
						Records: &[]dns.Record{{Content: ptr.To("1.1.1.1")}},
						Id:      ptr.To("some-uuid"),
						Ttl:     ptr.To[int64](300),
					},
					{
						Name:    ptr.To("test.example.com."),
						Active:  ptr.To(false),
						Type:    dns.RecordSetGetTypeAttributeType(ptr.To("A")),
						Records: &[]dns.Record{{Content: ptr.To("4.4.4.4")}},
						Id:      ptr.To("some-uuid2"),
						Ttl:     ptr.To[int64](300),
					},
				},
			}, nil)
			mockCreateRequest = mock.NewMockApiCreateRecordSetRequest(ctrl)
			mockUpdateRequest = mock.NewMockApiPartialUpdateRecordSetRequest(ctrl)
		})

		It("should create a new record set if it does not exist", func() {
			mockAPI.EXPECT().CreateRecordSet(ctx, client.projectID, "zone1").Return(mockCreateRequest)
			mockCreateRequest.EXPECT().CreateRecordSetPayload(dns.CreateRecordSetPayload{
				Name:    ptr.To("new.example.com."),
				Records: &[]dns.RecordPayload{{Content: ptr.To("1.1.1.1")}},
				Type:    ptr.To(dns.CreateRecordSetPayloadTypes("A")),
				Ttl:     ptr.To(int64(300)),
			}).Return(mockCreateRequest)
			mockCreateRequest.EXPECT().Execute().Return(nil, nil)

			Expect(client.CreateOrUpdateRecordSet(ctx, "zone1", "new.example.com.", "A", []string{"1.1.1.1"}, 300)).To(Succeed())
		})

		It("should update the existing record set if it exists and records are different", func() {
			mockAPI.EXPECT().PartialUpdateRecordSet(ctx, client.projectID, "zone1", "some-uuid").Return(mockUpdateRequest)
			mockUpdateRequest.EXPECT().PartialUpdateRecordSetPayload(dns.PartialUpdateRecordSetPayload{
				Name:    ptr.To("test.example.com."),
				Records: &[]dns.RecordPayload{{Content: ptr.To("4.4.4.4")}},
				Ttl:     ptr.To(int64(300)),
			}).Return(mockUpdateRequest)
			mockUpdateRequest.EXPECT().Execute().Return(nil, nil)

			Expect(client.CreateOrUpdateRecordSet(ctx, "zone1", "test.example.com.", "A", []string{"4.4.4.4"}, 300)).To(Succeed())
		})

		It("should do nothing if the existing record set has the same records and TTL", func() {
			Expect(client.CreateOrUpdateRecordSet(ctx, "zone1", "test.example.com.", "A", []string{"1.1.1.1"}, 300)).To(Succeed())
		})
	})

	Describe("Delete Record", func() {
		BeforeEach(func() {
			mockAPI.EXPECT().ListRecordSetsExecute(ctx, client.projectID, "zone1").Return(&dns.ListRecordSetsResponse{
				RrSets: &[]dns.RecordSet{{
					Name:   ptr.To("test.example.com."),
					Active: ptr.To(true),
					Type:   dns.RecordSetGetTypeAttributeType(ptr.To("A")),
					Id:     ptr.To("some-uuid"),
				}},
			}, nil)
		})

		It("should do nothing if the record set does not exist", func() {
			Expect(client.DeleteRecordSet(ctx, "zone1", "nonexistent.example.com.", "A")).To(Succeed())
		})

		It("should delete the record set if it exists", func() {
			mockAPI.EXPECT().DeleteRecordSetExecute(ctx, client.projectID, "zone1", "some-uuid").Return(nil, nil)

			Expect(client.DeleteRecordSet(ctx, "zone1", "test.example.com.", "A")).To(Succeed())
		})

		It("should delete the record even if a non-FQDN is specified", func() {
			mockAPI.EXPECT().DeleteRecordSetExecute(ctx, client.projectID, "zone1", "some-uuid").Return(nil, nil)

			Expect(client.DeleteRecordSet(ctx, "zone1", "test.example.com", "A")).To(Succeed())
		})
	})

	Describe("findRecordSet", func() {
		BeforeEach(func() {
			rrSets := []dns.RecordSet{
				{
					Name:    ptr.To("active.example.com."),
					Active:  ptr.To(true),
					Type:    dns.RecordSetGetTypeAttributeType(ptr.To("A")),
					Records: &[]dns.Record{{Content: ptr.To("1.1.1.1")}},
					Id:      ptr.To("active-a-uuid"),
				},
				{
					Name:    ptr.To("active2.example.com."),
					Active:  ptr.To(true),
					Type:    dns.RecordSetGetTypeAttributeType(ptr.To("A")),
					Records: &[]dns.Record{{Content: ptr.To("1.1.1.1")}},
					Id:      ptr.To("active2-a-uuid"),
				},
				{
					Name:    ptr.To("active.example.com."),
					Active:  ptr.To(true),
					Type:    dns.RecordSetGetTypeAttributeType(ptr.To("TXT")),
					Records: &[]dns.Record{{Content: ptr.To("hello-world")}},
					Id:      ptr.To("active-txt-uuid"),
				},
				{
					Name:    ptr.To("inactive.example.com."),
					Active:  ptr.To(false),
					Type:    dns.RecordSetGetTypeAttributeType(ptr.To("A")),
					Records: &[]dns.Record{{Content: ptr.To("2.2.2.2")}},
					Id:      ptr.To("inactive-a-uuid"),
				},
			}
			rand.Shuffle(len(rrSets), func(i, j int) {
				rrSets[i], rrSets[j] = rrSets[j], rrSets[i]
			})
			mockAPI.EXPECT().ListRecordSetsExecute(ctx, client.projectID, "zone1").Return(&dns.ListRecordSetsResponse{
				RrSets: &rrSets,
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
		[]dns.Record{{Content: ptr.To("1.2.3.4")}},
		[]string{"1.2.3.4"},
		true,
	),
	Entry("equal records, different order",
		[]dns.Record{{Content: ptr.To("1.2.3.4")}, {Content: ptr.To("5.6.7.8")}},
		[]string{"5.6.7.8", "1.2.3.4"},
		true,
	),
	Entry("different records",
		[]dns.Record{{Content: ptr.To("1.2.3.4")}},
		[]string{"5.6.7.8"},
		false,
	),
	Entry("subset records",
		[]dns.Record{{Content: ptr.To("1.2.3.4")}},
		[]string{"1.2.3.4", "5.6.7.8"},
		false,
	),
)
