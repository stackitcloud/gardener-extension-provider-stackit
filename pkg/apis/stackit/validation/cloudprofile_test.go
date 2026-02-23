// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"github.com/gardener/gardener/pkg/apis/core"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/utils/ptr"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/validation"
)

var _ = Describe("CloudProfileConfig validation", func() {
	Describe("#ValidateCloudProfileConfig", func() {
		var (
			cloudProfileConfig  *stackitv1alpha1.CloudProfileConfig
			machineImages       []core.MachineImage
			machineImageName    string
			machineImageVersion string
			fldPath             *field.Path
		)

		BeforeEach(func() {
			machineImageName = "ubuntu"
			machineImageVersion = "1.2.3"
			cloudProfileConfig = &stackitv1alpha1.CloudProfileConfig{
				Constraints: stackitv1alpha1.Constraints{
					FloatingPools: []stackitv1alpha1.FloatingPool{
						{Name: "MY-POOL"},
					},
				},
				DNSServers: []string{
					"1.2.3.4",
					"5.6.7.8",
				},
				KeyStoneURL: "http://url-to-keystone/v3",
				MachineImages: []stackitv1alpha1.MachineImages{
					{
						Name: machineImageName,
						Versions: []stackitv1alpha1.MachineImageVersion{
							{
								Version: machineImageVersion,
								Image:   "ubuntu-1.2.3",
								Regions: []stackitv1alpha1.RegionIDMapping{{
									Name:         "eu01",
									ID:           "9afa968b-ed9e-4ba0-a394-f74cbb0313w2",
									Architecture: ptr.To(v1beta1constants.ArchitectureAMD64),
								}},
							},
						},
					},
				},
			}
			machineImages = []core.MachineImage{
				{
					Name: machineImageName,
					Versions: []core.MachineImageVersion{
						{
							ExpirableVersion: core.ExpirableVersion{Version: machineImageVersion},
							Architectures:    []string{v1beta1constants.ArchitectureAMD64},
						},
					},
				},
			}
			fldPath = field.NewPath("root")
		})

		Context("floating pools constraints", func() {
			It("should enforce that at least one pool has been defined", func() {
				cloudProfileConfig.Constraints.FloatingPools = []stackitv1alpha1.FloatingPool{}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.constraints.floatingPools"),
				}))))
			})

			It("should forbid unsupported pools", func() {
				cloudProfileConfig.Constraints.FloatingPools = []stackitv1alpha1.FloatingPool{
					{
						Name:   "",
						Region: ptr.To(""),
						Domain: ptr.To(""),
					},
				}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.constraints.floatingPools[0].name"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.constraints.floatingPools[0].region"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.constraints.floatingPools[0].domain"),
				}))))
			})

			It("should forbid duplicates regions and domains in pools", func() {
				cloudProfileConfig.Constraints.FloatingPools = []stackitv1alpha1.FloatingPool{
					{
						Name:   "foo",
						Region: ptr.To("rfoo"),
					},
					{
						Name:   "foo",
						Region: ptr.To("rfoo"),
					},
					{
						Name:   "foo",
						Domain: ptr.To("dfoo"),
					},
					{
						Name:   "foo",
						Domain: ptr.To("dfoo"),
					},
					{
						Name:   "foo",
						Domain: ptr.To("dfoo"),
						Region: ptr.To("rfoo"),
					},
					{
						Name:   "foo",
						Domain: ptr.To("dfoo"),
						Region: ptr.To("rfoo"),
					},
				}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":     Equal(field.ErrorTypeDuplicate),
						"Field":    Equal("root.constraints.floatingPools[1].name"),
						"BadValue": Equal("foo"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":     Equal(field.ErrorTypeDuplicate),
						"Field":    Equal("root.constraints.floatingPools[3].name"),
						"BadValue": Equal("foo"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":     Equal(field.ErrorTypeDuplicate),
						"Field":    Equal("root.constraints.floatingPools[5].name"),
						"BadValue": Equal("foo"),
					}))))
			})
		})

		Context("keystone url validation", func() {
			It("should forbid keystone urls with unsupported format", func() {
				cloudProfileConfig.KeyStoneURL = ""
				cloudProfileConfig.KeyStoneURLs = nil

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.keyStoneURL"),
				}))))
			})

			It("should forbid keystone urls with missing keys", func() {
				cloudProfileConfig.KeyStoneURL = ""
				cloudProfileConfig.KeyStoneURLs = []stackitv1alpha1.KeyStoneURL{{}}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.keyStoneURLs[0].region"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.keyStoneURLs[0].url"),
				}))))
			})

			It("should forbid duplicate regions for keystone urls", func() {
				cloudProfileConfig.KeyStoneURL = ""
				cloudProfileConfig.KeyStoneURLs = []stackitv1alpha1.KeyStoneURL{
					{
						Region: "foo",
						URL:    "bar",
					},
					{
						Region: "foo",
						URL:    "bar",
					},
				}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeDuplicate),
					"Field": Equal("root.keyStoneURLs[1].region"),
				}))))
			})
		})

		It("should forbid invalid keystone CA Certs", func() {
			cloudProfileConfig.KeyStoneCACert = ptr.To("foo")

			errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)
			Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("root.caCert"),
				"Detail": Equal("caCert is not a valid PEM-encoded certificate"),
			}))))
		})

		Context("dns server validation", func() {
			It("should forbid not invalid dns server ips", func() {
				cloudProfileConfig.DNSServers = []string{"not-a-valid-ip"}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("root.dnsServers[0]"),
				}))))
			})
		})

		Context("dhcp domain validation", func() {
			It("should forbid not specifying a value when the key is present", func() {
				cloudProfileConfig.DHCPDomain = ptr.To("")

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.dhcpDomain"),
				}))))
			})
		})

		Context("machine image validation", func() {
			It("should pass validation", func() {
				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)
				Expect(errorList).To(BeEmpty())
			})

			It("should pass validation even without regions in the machineImage version", func() {
				cloudProfileConfig.MachineImages[0].Versions[0].Regions = nil
				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)
				Expect(errorList).To(BeEmpty())
			})

			It("should enforce that at least one machine image has been defined", func() {
				cloudProfileConfig.MachineImages = []stackitv1alpha1.MachineImages{}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.machineImages"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.machineImages[0]"),
				}))))
			})

			It("should forbid unsupported machine image configuration", func() {
				cloudProfileConfig.MachineImages = []stackitv1alpha1.MachineImages{{}}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.machineImages[0].name"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.machineImages[0].versions"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.machineImages[0]"),
				}))))
			})

			It("should forbid unsupported machine image version configuration", func() {
				cloudProfileConfig.MachineImages = []stackitv1alpha1.MachineImages{
					{
						Name:     "abc",
						Versions: []stackitv1alpha1.MachineImageVersion{{}},
					},
				}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.machineImages[0].versions[0].version"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.machineImages[0]"),
				}))))
			})

			It("should forbid missing architecture mapping", func() {
				machineImages[0].Versions[0].Architectures = []string{"arm64"}
				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("spec.machineImages[0].versions[0]"),
					})),
				))
			})

			It("should automatically use amd64", func() {
				cloudProfileConfig.MachineImages[0].Versions[0].Regions[0].Architecture = nil
				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)
				Expect(errorList).To(BeEmpty())
			})

			Context("region mapping validation", func() {
				It("should forbid empty region name", func() {
					cloudProfileConfig.MachineImages = []stackitv1alpha1.MachineImages{
						{
							Name: "abc",
							Versions: []stackitv1alpha1.MachineImageVersion{{
								Version: "foo",
								Regions: []stackitv1alpha1.RegionIDMapping{{
									ID: "abc_foo",
								}},
							}},
						},
					}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("root.machineImages[0].versions[0].regions[0].name"),
					})), PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("spec.machineImages[0]"),
					}))))
				})

				It("should forbid empty image ID", func() {
					cloudProfileConfig.MachineImages = []stackitv1alpha1.MachineImages{
						{
							Name: "abc",
							Versions: []stackitv1alpha1.MachineImageVersion{{
								Version: "foo",
								Regions: []stackitv1alpha1.RegionIDMapping{{
									Name: "eu01",
								}},
							}},
						},
					}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("root.machineImages[0].versions[0].regions[0].id"),
					})), PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("spec.machineImages[0]"),
					}))))
				})

				It("should forbid unknown architectures", func() {
					cloudProfileConfig.MachineImages = []stackitv1alpha1.MachineImages{
						{
							Name: "abc",
							Versions: []stackitv1alpha1.MachineImageVersion{{
								Version: "foo",
								Regions: []stackitv1alpha1.RegionIDMapping{
									{
										Name:         "eu01",
										ID:           "abc_foo_amd64",
										Architecture: ptr.To("amd64"),
									},
									{
										Name:         "eu01",
										ID:           "abc_foo_arm64",
										Architecture: ptr.To("arm64"),
									},
									{
										Name:         "eu01",
										ID:           "abc_foo_ppc64",
										Architecture: ptr.To("ppc64"),
									},
								},
							}},
						},
					}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeNotSupported),
						"Field": Equal("root.machineImages[0].versions[0].regions[2].architecture"),
					})), PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal("spec.machineImages[0]"),
					}))))
				})
			})
		})

		Context("server group policy validation", func() {
			It("should forbid empty server group policy", func() {
				cloudProfileConfig.ServerGroupPolicies = []string{
					"affinity",
					"",
				}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.serverGroupPolicies[1]"),
				}))))
			})
		})
	})
})
