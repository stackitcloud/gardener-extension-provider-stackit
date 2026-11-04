// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package infrastructure_test

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/stackitcloud/gardener-extension-provider-stackit/v2/pkg/controller/infrastructure"
)

var _ = Describe("Add", func() {
	Describe("SetDefaultRateLimiter", func() {
		It("should set default rate limiter with 1 minute base delay and 30 minutes max delay when none is configured", func() {
			options := infrastructure.AddOptions{}
			infrastructure.SetDefaultRateLimiter(&options)

			Expect(options.Controller.RateLimiter).ToNot(BeNil())

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "shoot--project--test",
					Name:      "infrastructure",
				},
			}

			// First failure should back off for 1 minute
			delay1 := options.Controller.RateLimiter.When(req)
			Expect(delay1).To(Equal(1 * time.Minute))

			// Second failure should double to 2 minutes
			delay2 := options.Controller.RateLimiter.When(req)
			Expect(delay2).To(Equal(2 * time.Minute))

			// Third failure should double to 4 minutes
			delay3 := options.Controller.RateLimiter.When(req)
			Expect(delay3).To(Equal(4 * time.Minute))

			// Forget should reset the backoff
			options.Controller.RateLimiter.Forget(req)
			delayAfterReset := options.Controller.RateLimiter.When(req)
			Expect(delayAfterReset).To(Equal(1 * time.Minute))
		})

		It("should preserve existing rate limiter if already configured", func() {
			customLimiter := workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](
				5*time.Second,
				10*time.Second,
			)

			options := infrastructure.AddOptions{
				Controller: controller.Options{
					RateLimiter: customLimiter,
				},
			}

			infrastructure.SetDefaultRateLimiter(&options)
			Expect(options.Controller.RateLimiter).To(BeIdenticalTo(customLimiter))

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "shoot--project--test",
					Name:      "infrastructure",
				},
			}
			Expect(options.Controller.RateLimiter.When(req)).To(Equal(5 * time.Second))
		})
	})
})
