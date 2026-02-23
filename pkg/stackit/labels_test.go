package stackit_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	. "github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/stackit"
)

var _ = Describe("ToLabels", func() {
	It("should correctly convert the labels", func() {
		Expect(ToLabels(map[string]string{
			"foo": "bar",
			"bar": "baz",
		})).To(Equal(map[string]any{
			"foo": "bar",
			"bar": "baz",
		}))
	})
})

var _ = Describe("LabelSelector", func() {
	var selector LabelSelector

	BeforeEach(func() {
		selector = LabelSelector{
			"foo": "bar",
		}
	})

	It("should require the selector's labels", func() {
		Expect(selector.Matches(map[string]any{"foo": "bar"})).To(BeTrue())
		Expect(selector.Matches(map[string]any{"foo": "nope"})).To(BeFalse())
		Expect(selector.Matches(map[string]any{"foo": nil})).To(BeFalse())
		Expect(selector.Matches(map[string]any{"bar": "foo"})).To(BeFalse())
	})

	It("should ignore additional labels", func() {
		Expect(selector.Matches(map[string]any{
			"foo": "bar",
			"bar": "baz",
			"boo": nil,
		})).To(BeTrue())
		Expect(selector.Matches(map[string]any{
			"foo": "nope",
			"bar": "baz",
			"boo": nil,
		})).To(BeFalse())
		Expect(selector.Matches(map[string]any{
			"bar": "baz",
			"boo": nil,
		})).To(BeFalse())
	})
})
