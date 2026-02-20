package dnsrecord

import (
	"context"
	"errors"
	"fmt"

	"github.com/gardener/gardener/extensions/pkg/controller"
	"github.com/gardener/gardener/extensions/pkg/controller/dnsrecord"
	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	gardencorev1beta1helper "github.com/gardener/gardener/pkg/apis/core/v1beta1/helper"
	extensionsv1alpha1 "github.com/gardener/gardener/pkg/apis/extensions/v1alpha1"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit"
	stackitclient "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client"
	mock "github.com/stackitcloud/gardener-extension-provider-stackit/pkg/stackit/client/mock"
)

const (
	name        = "stackit-external"
	namespace   = "shoot--foobar--stackit"
	shootDomain = "shoot.example.com"
	domainName  = "api.stackit.foobar." + shootDomain
	zone        = "zone"
	address     = "1.2.3.4"
)

var _ = Describe("Actuator", func() {
	var (
		ctx    context.Context
		logger logr.Logger

		ctrl    *gomock.Controller
		c       client.Client
		dnsMock *mock.MockDNSClient

		a dnsrecord.Actuator

		dns     *extensionsv1alpha1.DNSRecord
		cluster *controller.Cluster
		zones   []stackitclient.DNSZone
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		dnsMock = mock.NewMockDNSClient(ctrl)

		dns = &extensionsv1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: namespace,
			},
			Spec: extensionsv1alpha1.DNSRecordSpec{
				DefaultSpec: extensionsv1alpha1.DefaultSpec{
					Type: stackit.Type,
				},
				SecretRef: corev1.SecretReference{
					Name:      name,
					Namespace: namespace,
				},
				Name:       domainName,
				RecordType: extensionsv1alpha1.DNSRecordTypeA,
				Values:     []string{address},
			},
		}
		cluster = nil // this is not used by the fake dnsClientFunc

		c = fake.NewClientBuilder().
			WithScheme(kubernetes.SeedScheme).
			WithStatusSubresource(&extensionsv1alpha1.DNSRecord{}).
			WithObjects(dns).
			Build()

		logger = log.Log.WithName("test")

		a = &actuator{
			client: c,
			dnsClientFunc: func(_ context.Context, _ *extensionsv1alpha1.DNSRecord, _ *controller.Cluster) (stackitclient.DNSClient, error) {
				return dnsMock, nil
			},
		}

		zones = []stackitclient.DNSZone{
			{ID: zone, DNSName: shootDomain},
			{ID: "zone2", DNSName: "example.com"},
			{ID: "zone3", DNSName: "other.com"},
		}
	})

	Describe("#Reconcile", func() {
		It("should reconcile the DNSRecord", func() {
			dnsMock.EXPECT().ListZones(ctx).Return(zones, nil)
			dnsMock.EXPECT().CreateOrUpdateRecordSet(ctx, zone, domainName, string(extensionsv1alpha1.DNSRecordTypeA), []string{address}, int64(120)).
				Return(nil)

			Expect(a.Reconcile(ctx, logger, dns, cluster)).To(Succeed())

			Expect(c.Get(ctx, client.ObjectKeyFromObject(dns), dns)).To(Succeed())
			Expect(*dns.Status.Zone).To(Equal(zone))
		})

		It("should fail if creating the DNS record set failed", func() {
			dns.Spec.Zone = ptr.To(zone)
			dnsMock.EXPECT().CreateOrUpdateRecordSet(ctx, zone, domainName, string(extensionsv1alpha1.DNSRecordTypeA), []string{address}, int64(120)).
				Return(errors.New("test"))

			Expect(a.Reconcile(ctx, logger, dns, cluster)).To(HaveOccurred())
		})

		It("should fail with ERR_CONFIGURATION_PROBLEM if there is no such hosted zone", func() {
			dnsMock.EXPECT().ListZones(ctx).Return([]stackitclient.DNSZone{}, nil)

			err := a.Reconcile(ctx, logger, dns, cluster)
			Expect(err).To(HaveOccurred())
			coder, ok := err.(gardencorev1beta1helper.Coder)
			Expect(ok).To(BeTrue())
			Expect(coder.Codes()).To(Equal([]gardencorev1beta1.ErrorCode{gardencorev1beta1.ErrorConfigurationProblem}))
		})

		It("should fail with ERR_CONFIGURATION_PROBLEM if the hosted zone was deleted", func() {
			dns.Spec.Zone = ptr.To(zone)
			// This error is returned when the zone was deleted, but can still be re-activated
			dnsMock.EXPECT().CreateOrUpdateRecordSet(ctx, zone, domainName, string(extensionsv1alpha1.DNSRecordTypeA), []string{address}, int64(120)).
				Return(&stackitclient.Error{
					Message:    fmt.Sprintf("zone is not ready for record set %s", domainName),
					StatusCode: 400,
				})

			err := a.Reconcile(ctx, logger, dns, cluster)
			Expect(err).To(HaveOccurred())
			coder, ok := err.(gardencorev1beta1helper.Coder)
			Expect(ok).To(BeTrue())
			Expect(coder.Codes()).To(Equal([]gardencorev1beta1.ErrorCode{gardencorev1beta1.ErrorConfigurationProblem}))
		})
	})

	Describe("#Delete", func() {
		It("should not fail when there is a not found error", func() {
			dns.Spec.Zone = ptr.To(zone)
			dnsMock.EXPECT().DeleteRecordSet(ctx, zone, domainName, string(extensionsv1alpha1.DNSRecordTypeA)).
				Return(stackitclient.NewNotFoundError("foo", "bar"))

			Expect(a.Delete(ctx, logger, dns, cluster)).To(Succeed())
		})

		It("should delete the DNSRecord", func() {
			dns.Status.Zone = ptr.To(zone)
			dnsMock.EXPECT().DeleteRecordSet(ctx, zone, domainName, string(extensionsv1alpha1.DNSRecordTypeA)).
				Return(nil)

			err := a.Delete(ctx, logger, dns, nil)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
