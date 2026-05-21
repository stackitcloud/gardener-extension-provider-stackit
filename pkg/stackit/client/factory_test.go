package client

import (
	"crypto/tls"
	"encoding/pem"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("InjectCAIntoHTTPClient", func() {
	var (
		testServer *httptest.Server
		httpClient *http.Client
	)

	BeforeEach(func() {
		testServer = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		httpClient = &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{},
			},
		}
	})

	AfterEach(func() {
		testServer.Close()
	})

	Context("when the custom CA is injected into the HTTP client", func() {
		It("should successfully connect to the TLS server without certificate errors", func() {
			serverCert := testServer.Certificate()

			pemBlock := &pem.Block{
				Type:  "CERTIFICATE",
				Bytes: serverCert.Raw,
			}
			serverCertPEM := pem.EncodeToMemory(pemBlock)

			err := InjectCAIntoHTTPClient(httpClient, string(serverCertPEM))
			Expect(err).NotTo(HaveOccurred(), "InjectCAIntoHTTPClient should not return an error")

			resp, err := httpClient.Get(testServer.URL)

			Expect(err).NotTo(HaveOccurred(), "The client should trust the server's certificate")
			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})
	})
})
