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

			// Fourth failure: 8 minutes
			delay4 := options.Controller.RateLimiter.When(req)
			Expect(delay4).To(Equal(8 * time.Minute))

			// Fifth failure: 16 minutes
			delay5 := options.Controller.RateLimiter.When(req)
			Expect(delay5).To(Equal(16 * time.Minute))

			// Sixth failure: capped at maxDelay of 30 minutes (not 32 minutes)
			delay6 := options.Controller.RateLimiter.When(req)
			Expect(delay6).To(Equal(30 * time.Minute))

			// Seventh failure: still capped at 30 minutes
			delay7 := options.Controller.RateLimiter.When(req)
			Expect(delay7).To(Equal(30 * time.Minute))

			// Forget should reset the backoff
			options.Controller.RateLimiter.Forget(req)
			delayAfterReset := options.Controller.RateLimiter.When(req)
			Expect(delayAfterReset).To(Equal(1 * time.Minute))
		})

		It("should track failure backoffs independently for different shoot requests", func() {
			options := infrastructure.AddOptions{}
			infrastructure.SetDefaultRateLimiter(&options)

			reqShootA := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "shoot--project--a",
					Name:      "infrastructure",
				},
			}
			reqShootB := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "shoot--project--b",
					Name:      "infrastructure",
				},
			}

			// Shoot A fails twice (1m, then 2m)
			Expect(options.Controller.RateLimiter.When(reqShootA)).To(Equal(1 * time.Minute))
			Expect(options.Controller.RateLimiter.When(reqShootA)).To(Equal(2 * time.Minute))

			// Shoot B fails for the first time: should start at 1m, not affected by Shoot A
			Expect(options.Controller.RateLimiter.When(reqShootB)).To(Equal(1 * time.Minute))
		})

		It("should integrate correctly with a rate-limiting workqueue", func() {
			options := infrastructure.AddOptions{}
			infrastructure.SetDefaultRateLimiter(&options)

			queue := workqueue.NewTypedRateLimitingQueue(options.Controller.RateLimiter)
			defer queue.ShutDown()

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "shoot--project--test",
					Name:      "infrastructure",
				},
			}

			Expect(queue.NumRequeues(req)).To(Equal(0))

			// Simulate controller-runtime failing a reconcile and enqueuing with rate limiting
			queue.AddRateLimited(req)
			Expect(queue.NumRequeues(req)).To(Equal(1))

			queue.AddRateLimited(req)
			Expect(queue.NumRequeues(req)).To(Equal(2))

			// Simulate successful reconcile calling Forget
			queue.Forget(req)
			Expect(queue.NumRequeues(req)).To(Equal(0))
		})

		It("should ensure failure backoff waits at least 30 seconds (1 minute default) and not 1-2 seconds", func() {
			options := infrastructure.AddOptions{}
			infrastructure.SetDefaultRateLimiter(&options)

			req := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: "shoot--project--test",
					Name:      "infrastructure",
				},
			}

			// Controller-runtime default rate limiter retries in 5ms (almost immediately, <= 1-2 seconds)
			defaultRateLimiter := workqueue.DefaultTypedControllerRateLimiter[reconcile.Request]()
			Expect(defaultRateLimiter.When(req)).To(BeNumerically("<", 1*time.Second))

			// SetDefaultRateLimiter ensures the initial retry delay is 1 minute (>= 30 seconds),
			// preventing immediate retries while cloud resources are still being created
			initialDelay := options.Controller.RateLimiter.When(req)
			Expect(initialDelay).To(BeNumerically(">=", 30*time.Second))
			Expect(initialDelay).To(Equal(1 * time.Minute))

			// Reset rate limiter for the request before passing to workqueue
			options.Controller.RateLimiter.Forget(req)

			// Verify with workqueue that the failed request is delayed and NOT available immediately
			queue := workqueue.NewTypedRateLimitingQueue(options.Controller.RateLimiter)
			defer queue.ShutDown()

			queue.AddRateLimited(req)
			// Item should be in the delayed queue, not immediately ready in the queue
			Expect(queue.Len()).To(Equal(0))
			Expect(queue.NumRequeues(req)).To(Equal(1))
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
