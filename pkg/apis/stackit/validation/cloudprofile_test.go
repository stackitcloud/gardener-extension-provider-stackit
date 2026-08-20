// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package validation_test

import (
	"github.com/gardener/gardener/pkg/apis/core"
	"github.com/gardener/gardener/pkg/apis/core/v1beta1"
	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	"github.com/onsi/gomega/types"
	"k8s.io/apimachinery/pkg/util/validation/field"

	stackitv1alpha1 "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/v1alpha1"
	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/apis/stackit/validation"
)

var _ = Describe("CloudProfileConfig validation", func() {
	DescribeTableSubtree("#ValidateCloudProfileConfig", func(isCapabilitiesCloudProfile bool) {
		var (
			capabilityDefinitions []v1beta1.CapabilityDefinition
			cloudProfileConfig    *stackitv1alpha1.CloudProfileConfig
			machineImages         []core.MachineImage
			machineImageName      string
			machineImageVersion   string
			fldPath               *field.Path
		)

		BeforeEach(func() {
			regions := []stackitv1alpha1.RegionIDMapping{{
				Name: "eu01",
				ID:   "9afa968b-ed9e-4ba0-a394-f74cbb0313w2",
			}}
			var capabilityFlavors []stackitv1alpha1.MachineImageFlavor

			if isCapabilitiesCloudProfile {
				capabilityDefinitions = []v1beta1.CapabilityDefinition{{
					Name:   v1beta1constants.ArchitectureName,
					Values: []string{"amd64"},
				}}
				capabilityFlavors = []stackitv1alpha1.MachineImageFlavor{{
					Regions: regions,
					Capabilities: v1beta1.Capabilities{
						v1beta1constants.ArchitectureName: []string{"amd64"},
					}}}
				regions = nil
			} else {
				regions[0].Architecture = new("amd64")
			}
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
								Version:           machineImageVersion,
								Image:             "ubuntu-1.2.3",
								Regions:           regions,
								CapabilityFlavors: capabilityFlavors,
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
			It("should forbid unsupported pools", func() {
				//nolint:staticcheck // SA1019: needed for migration purposes
				cloudProfileConfig.Constraints.FloatingPools = []stackitv1alpha1.FloatingPool{
					{
						Name:   "",
						Region: new(""),
						Domain: new(""),
					},
				}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

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
				//nolint:staticcheck // SA1019: needed for migration purposes
				cloudProfileConfig.Constraints.FloatingPools = []stackitv1alpha1.FloatingPool{
					{
						Name:   "foo",
						Region: new("rfoo"),
					},
					{
						Name:   "foo",
						Region: new("rfoo"),
					},
					{
						Name:   "foo",
						Domain: new("dfoo"),
					},
					{
						Name:   "foo",
						Domain: new("dfoo"),
					},
					{
						Name:   "foo",
						Domain: new("dfoo"),
						Region: new("rfoo"),
					},
					{
						Name:   "foo",
						Domain: new("dfoo"),
						Region: new("rfoo"),
					},
				}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

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
			It("should forbid keystone urls with missing keys", func() {
				//nolint:staticcheck // SA1019: needed for migration purposes
				cloudProfileConfig.KeyStoneURL = ""
				//nolint:staticcheck // SA1019: needed for migration purposes
				cloudProfileConfig.KeyStoneURLs = []stackitv1alpha1.KeyStoneURL{{}}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.keyStoneURLs[0].region"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.keyStoneURLs[0].url"),
				}))))
			})

			It("should forbid duplicate regions for keystone urls", func() {
				//nolint:staticcheck // SA1019: needed for migration purposes
				cloudProfileConfig.KeyStoneURL = ""
				//nolint:staticcheck // SA1019: needed for migration purposes
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

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeDuplicate),
					"Field": Equal("root.keyStoneURLs[1].region"),
				}))))
			})
		})

		It("should forbid invalid keystone CA Certs", func() {
			//nolint:staticcheck // SA1019: needed for migration purposes
			cloudProfileConfig.KeyStoneCACert = new("foo")

			errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
			Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("root.caCert"),
				"Detail": Equal("caCert is not a valid PEM-encoded certificate"),
			}))))
		})

		Context("dns server validation", func() {
			It("should forbid not invalid dns server ips", func() {
				cloudProfileConfig.DNSServers = []string{"not-a-valid-ip"}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("root.dnsServers[0]"),
				}))))
			})
		})

		Context("dhcp domain validation", func() {
			It("should forbid not specifying a value when the key is present", func() {
				//nolint:staticcheck // SA1019: needed for migration purposes
				cloudProfileConfig.DHCPDomain = new("")

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.dhcpDomain"),
				}))))
			})
		})

		Context("machine image validation", func() {
			It("should pass validation", func() {
				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
				Expect(errorList).To(BeEmpty())
			})

			It("should pass validation even without regions in the machineImage version", func() {
				cloudProfileConfig.MachineImages[0].Versions[0].Regions = nil
				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
				Expect(errorList).To(BeEmpty())
			})

			It("should enforce that at least one machine image has been defined", func() {
				cloudProfileConfig.MachineImages = []stackitv1alpha1.MachineImages{}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

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

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

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

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.machineImages[0].versions[0].version"),
				})), PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.machineImages[0]"),
				}))))
			})

			It("should forbid missing architecture or capabilitySet mapping", func() {
				var fieldMatcher types.GomegaMatcher
				if isCapabilitiesCloudProfile {
					machineImages[0].Versions[0].CapabilityFlavors = []core.MachineImageFlavor{
						{Capabilities: core.Capabilities{v1beta1constants.ArchitectureName: []string{"arm64"}}},
					}
					fieldMatcher = Equal("spec.machineImages[0].versions[0].capabilityFlavors[0]")
				} else {
					machineImages[0].Versions[0].Architectures = []string{"arm64"}
					fieldMatcher = Equal("spec.machineImages[0].versions[0]")
				}
				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{"Type": Equal(field.ErrorTypeRequired), "Field": fieldMatcher})),
				))
			})
			It("should automatically use amd64 (or default to capabilityDefinitions)", func() {
				if !isCapabilitiesCloudProfile {
					cloudProfileConfig.MachineImages[0].Versions[0].Regions[0].Architecture = nil
				}
				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
				Expect(errorList).To(BeEmpty())
			})

			Context("region mapping validation", func() {
				It("should forbid empty region name", func() {
					var fieldMatcher string
					var regions = []stackitv1alpha1.RegionIDMapping{{
						ID: "abc_foo",
					}}

					if isCapabilitiesCloudProfile {
						fieldMatcher = "root.machineImages[0].versions[0].capabilityFlavors[0].regions[0].name"
						cloudProfileConfig.MachineImages[0].Versions[0].CapabilityFlavors[0].Regions = regions
					} else {
						fieldMatcher = "root.machineImages[0].versions[0].regions[0].name"
						cloudProfileConfig.MachineImages[0].Versions[0].Regions = regions
					}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal(fieldMatcher),
					}))))
				})

				It("should forbid empty image ID", func() {
					var fieldMatcher string

					var regions = []stackitv1alpha1.RegionIDMapping{{
						Name: "eu01",
					}}
					if isCapabilitiesCloudProfile {
						fieldMatcher = "root.machineImages[0].versions[0].capabilityFlavors[0].regions[0].id"
						cloudProfileConfig.MachineImages[0].Versions[0].CapabilityFlavors[0].Regions = regions
					} else {
						fieldMatcher = "root.machineImages[0].versions[0].regions[0].id"
						cloudProfileConfig.MachineImages[0].Versions[0].Regions = regions
					}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": Equal(fieldMatcher),
					}))))
				})

				It("should forbid unknown architectures", func() {

					var notSupportedField, requiredField types.GomegaMatcher
					if isCapabilitiesCloudProfile {
						cloudProfileConfig.MachineImages[0].Versions[0].CapabilityFlavors[0].Capabilities = v1beta1.Capabilities{
							v1beta1constants.ArchitectureName: []string{"foo"},
						}
						notSupportedField = Equal("root.machineImages[0].versions[0].capabilityFlavors[0].capabilities.architecture[0]")
						requiredField = Equal("spec.machineImages[0].versions[0].capabilityFlavors[0]")
					} else {
						cloudProfileConfig.MachineImages[0].Versions[0].Regions[0].Architecture = new("foo")
						notSupportedField = Equal("root.machineImages[0].versions[0].regions[0].architecture")
						requiredField = Equal("spec.machineImages[0].versions[0]")

					}
					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeNotSupported),
						"Field": notSupportedField,
					})), PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeRequired),
						"Field": requiredField,
					}))))

				})
			})

			Context("mixed format validation (capabilities CloudProfile)", func() {
				BeforeEach(func() {
					if !isCapabilitiesCloudProfile {
						Skip("mixed format tests only apply to capabilities CloudProfiles")
					}
				})

				It("should allow old-format regions in a capabilities CloudProfile", func() {
					// Use old format (regions with architecture) instead of capabilityFlavors
					cloudProfileConfig.MachineImages[0].Versions[0].CapabilityFlavors = nil
					cloudProfileConfig.MachineImages[0].Versions[0].Regions = []stackitv1alpha1.RegionIDMapping{{
						Name:         "eu01",
						ID:           "ubuntu-amd64-id",
						Architecture: new("amd64"),
					}}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
					Expect(errorList).To(BeEmpty())
				})

				It("should forbid both regions AND capabilityFlavors on the same version", func() {
					cloudProfileConfig.MachineImages[0].Versions[0].Regions = []stackitv1alpha1.RegionIDMapping{{
						Name:         "eu01",
						ID:           "ubuntu-amd64-id",
						Architecture: new("amd64"),
					}}
					// capabilityFlavors is already set from BeforeEach

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeForbidden),
						"Field": Equal("root.machineImages[0].versions[0]"),
					}))))
				})

				It("should allow mixed format across versions within the same image", func() {
					capabilityDefinitions = []v1beta1.CapabilityDefinition{{
						Name:   v1beta1constants.ArchitectureName,
						Values: []string{"amd64", "arm64"},
					}}
					// Version 1.2.3: old format (regions with architecture)
					cloudProfileConfig.MachineImages[0].Versions[0].CapabilityFlavors = nil
					cloudProfileConfig.MachineImages[0].Versions[0].Regions = []stackitv1alpha1.RegionIDMapping{
						{Name: "eu01", ID: "ubuntu-1.2.3-amd64", Architecture: new("amd64")},
						{Name: "eu01", ID: "ubuntu-1.2.3-arm64", Architecture: new("arm64")},
					}
					// Version 2.0.0: new format (capabilityFlavors)
					cloudProfileConfig.MachineImages[0].Versions = append(cloudProfileConfig.MachineImages[0].Versions, stackitv1alpha1.MachineImageVersion{
						Version: "2.0.0",
						CapabilityFlavors: []stackitv1alpha1.MachineImageFlavor{
							{
								Regions:      []stackitv1alpha1.RegionIDMapping{{Name: "eu01", ID: "ubuntu-2.0.0-amd64"}},
								Capabilities: v1beta1.Capabilities{v1beta1constants.ArchitectureName: []string{"amd64"}},
							},
							{
								Regions:      []stackitv1alpha1.RegionIDMapping{{Name: "eu01", ID: "ubuntu-2.0.0-arm64"}},
								Capabilities: v1beta1.Capabilities{v1beta1constants.ArchitectureName: []string{"arm64"}},
							},
						},
					})

					// Spec version for 1.2.3: uses CapabilityFlavors with separate per-arch entries
					machineImages[0].Versions[0].CapabilityFlavors = []core.MachineImageFlavor{
						{Capabilities: core.Capabilities{v1beta1constants.ArchitectureName: []string{"amd64"}}},
						{Capabilities: core.Capabilities{v1beta1constants.ArchitectureName: []string{"arm64"}}},
					}
					// Spec version for 2.0.0: also uses CapabilityFlavors
					machineImages[0].Versions = append(machineImages[0].Versions, core.MachineImageVersion{
						ExpirableVersion: core.ExpirableVersion{Version: "2.0.0"},
						CapabilityFlavors: []core.MachineImageFlavor{
							{Capabilities: core.Capabilities{v1beta1constants.ArchitectureName: []string{"amd64"}}},
							{Capabilities: core.Capabilities{v1beta1constants.ArchitectureName: []string{"arm64"}}},
						},
					})

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
					Expect(errorList).To(BeEmpty())
				})

				It("should fail when old-format regions are missing a required architecture", func() {
					capabilityDefinitions = []v1beta1.CapabilityDefinition{{
						Name:   v1beta1constants.ArchitectureName,
						Values: []string{"amd64", "arm64"},
					}}
					// Old format regions only provide amd64
					cloudProfileConfig.MachineImages[0].Versions[0].CapabilityFlavors = nil
					cloudProfileConfig.MachineImages[0].Versions[0].Regions = []stackitv1alpha1.RegionIDMapping{{
						Name:         "eu01",
						ID:           "ubuntu-amd64-id",
						Architecture: new("amd64"),
					}}
					// Spec requires both amd64 and arm64 via separate CapabilityFlavors
					machineImages[0].Versions[0].CapabilityFlavors = []core.MachineImageFlavor{
						{Capabilities: core.Capabilities{v1beta1constants.ArchitectureName: []string{"amd64"}}},
						{Capabilities: core.Capabilities{v1beta1constants.ArchitectureName: []string{"arm64"}}},
					}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(field.ErrorTypeRequired),
						"Field":  Equal("spec.machineImages[0].versions[0].capabilityFlavors[1]"),
						"Detail": ContainSubstring("arm64"),
					}))))
				})

				It("should default architecture to amd64 for old-format regions without explicit architecture", func() {
					cloudProfileConfig.MachineImages[0].Versions[0].CapabilityFlavors = nil
					cloudProfileConfig.MachineImages[0].Versions[0].Regions = []stackitv1alpha1.RegionIDMapping{{
						Name: "eu01",
						ID:   "ubuntu-amd64-id",
						// No Architecture set - should default to amd64
					}}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
					Expect(errorList).To(BeEmpty())
				})

				It("should forbid architecture field in capability flavor regions", func() {
					cloudProfileConfig.MachineImages[0].Versions[0].CapabilityFlavors[0].Regions = []stackitv1alpha1.RegionIDMapping{{
						Name:         "eu01",
						ID:           "ubuntu-amd64-id",
						Architecture: new("amd64"),
					}}

					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)
					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeForbidden),
						"Field": Equal("root.machineImages[0].versions[0].capabilityFlavors[0].regions[0].architecture"),
					}))))
				})

				It("should forbid capabilityFlavors in a non-capabilities CloudProfile", func() {
					// This test verifies the inverse: capabilityFlavors are forbidden when no capabilityDefinitions
					errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, nil, fldPath)
					Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeForbidden),
						"Field": Equal("root.machineImages[0].versions[0].capabilityFlavors"),
					}))))
				})
			})
		})

		Context("server group policy validation", func() {
			It("should forbid empty server group policy", func() {
				//nolint:staticcheck // SA1019: needed for migration purposes
				cloudProfileConfig.ServerGroupPolicies = []string{
					"affinity",
					"",
				}

				errorList := ValidateCloudProfileConfig(cloudProfileConfig, machineImages, capabilityDefinitions, fldPath)

				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("root.serverGroupPolicies[1]"),
				}))))
			})
		})
	},
		Entry("CloudProfile uses regions only", false),
		Entry("CloudProfile uses capabilities", true))
})
