/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ironic

import (
	"context"
	"fmt"
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/gophercloud/gophercloud/v2"
)

const (
	oldEndpoint = "http://old:6385/v1/"
	newEndpoint = "http://new:6385/v1/"
)

var _ = Describe("isAuthError", func() {
	It("should return false for nil error", func() {
		Expect(isAuthError(nil)).To(BeFalse())
	})

	It("should return false for generic errors", func() {
		Expect(isAuthError(fmt.Errorf("some random error"))).To(BeFalse())
	})

	It("should return true for 401 ErrUnexpectedResponseCode", func() {
		err := gophercloud.ErrUnexpectedResponseCode{
			Actual:   http.StatusUnauthorized,
			Expected: []int{http.StatusOK},
		}
		Expect(isAuthError(err)).To(BeTrue())
	})

	It("should return false for 404 ErrUnexpectedResponseCode", func() {
		err := gophercloud.ErrUnexpectedResponseCode{
			Actual:   http.StatusNotFound,
			Expected: []int{http.StatusOK},
		}
		Expect(isAuthError(err)).To(BeFalse())
	})

	It("should return false for 500 ErrUnexpectedResponseCode", func() {
		err := gophercloud.ErrUnexpectedResponseCode{
			Actual:   http.StatusInternalServerError,
			Expected: []int{http.StatusOK},
		}
		Expect(isAuthError(err)).To(BeFalse())
	})

	It("should return true for *ErrUnableToReauthenticate (pointer, as gophercloud returns)", func() {
		err := &gophercloud.ErrUnableToReauthenticate{
			ErrOriginal: fmt.Errorf("original"),
			ErrReauth:   fmt.Errorf("reauth failed"),
		}
		Expect(isAuthError(err)).To(BeTrue())
	})

	It("should return true for *ErrErrorAfterReauthentication (pointer, as gophercloud returns)", func() {
		err := &gophercloud.ErrErrorAfterReauthentication{
			ErrOriginal: fmt.Errorf("still failing"),
		}
		Expect(isAuthError(err)).To(BeTrue())
	})

	It("should return true for wrapped *ErrUnableToReauthenticate", func() {
		inner := &gophercloud.ErrUnableToReauthenticate{
			ErrOriginal: fmt.Errorf("original"),
			ErrReauth:   fmt.Errorf("reauth failed"),
		}
		wrapped := fmt.Errorf("operation failed: %w", inner)
		Expect(isAuthError(wrapped)).To(BeTrue())
	})

	It("should return true for wrapped *ErrErrorAfterReauthentication", func() {
		inner := &gophercloud.ErrErrorAfterReauthentication{
			ErrOriginal: fmt.Errorf("still failing"),
		}
		wrapped := fmt.Errorf("operation failed: %w", inner)
		Expect(isAuthError(wrapped)).To(BeTrue())
	})

	It("should return true for wrapped 401 error", func() {
		inner := gophercloud.ErrUnexpectedResponseCode{
			Actual:   http.StatusUnauthorized,
			Expected: []int{http.StatusOK},
		}
		wrapped := fmt.Errorf("operation failed: %w", inner)
		Expect(isAuthError(wrapped)).To(BeTrue())
	})

	It("should return false for wrapped non-auth error", func() {
		inner := gophercloud.ErrUnexpectedResponseCode{
			Actual:   http.StatusNotFound,
			Expected: []int{http.StatusOK},
		}
		wrapped := fmt.Errorf("operation failed: %w", inner)
		Expect(isAuthError(wrapped)).To(BeFalse())
	})
})

var _ = Describe("reconnect", func() {
	It("should swap the service client on success", func() {
		oldSC := &gophercloud.ServiceClient{Endpoint: oldEndpoint}
		newSC := &gophercloud.ServiceClient{Endpoint: newEndpoint}

		c := &Client{
			serviceClient: oldSC,
			newServiceClient: func(context.Context) (*gophercloud.ServiceClient, error) {
				return newSC, nil
			},
		}

		Expect(c.reconnect(context.Background())).To(Succeed())
		Expect(c.serviceClient).To(Equal(newSC))
		Expect(c.GetEndpoint()).To(Equal(newEndpoint))
	})

	It("should return error when factory fails", func() {
		oldSC := &gophercloud.ServiceClient{Endpoint: oldEndpoint}

		c := &Client{
			serviceClient: oldSC,
			newServiceClient: func(context.Context) (*gophercloud.ServiceClient, error) {
				return nil, fmt.Errorf("keystone is down")
			},
		}

		err := c.reconnect(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("keystone is down"))
		Expect(c.serviceClient).To(Equal(oldSC), "should keep old client on failure")
	})

	It("should return error when factory returns nil client without error", func() {
		oldSC := &gophercloud.ServiceClient{Endpoint: oldEndpoint}

		c := &Client{
			serviceClient: oldSC,
			newServiceClient: func(context.Context) (*gophercloud.ServiceClient, error) {
				return nil, nil
			},
		}

		err := c.reconnect(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("nil service client"))
		Expect(c.serviceClient).To(Equal(oldSC), "should keep old client on nil return")
	})

	It("should return error when new client has empty endpoint", func() {
		oldSC := &gophercloud.ServiceClient{Endpoint: oldEndpoint}

		c := &Client{
			serviceClient: oldSC,
			newServiceClient: func(context.Context) (*gophercloud.ServiceClient, error) {
				return &gophercloud.ServiceClient{Endpoint: ""}, nil
			},
		}

		err := c.reconnect(context.Background())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("no endpoint configured"))
	})
})
