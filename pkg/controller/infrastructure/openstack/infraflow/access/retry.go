// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package access

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gophercloud/gophercloud/v2"
)

// following https://github.com/terraform-provider-openstack/terraform-provider-openstack/blob/cec35ae29769b4de7d84980b1335a2b723ffb15f/openstack/networking_v2_shared.go

type neutronErrorWrap struct {
	NeutronError neutronError
}

type neutronError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Detail  string `json:"detail"`
}

func retryOnError(log logr.Logger, err error) bool {
	switch {
	case gophercloud.ResponseCodeIs(err, http.StatusConflict):
		neutronError, e := decodeNeutronError(err)
		if e != nil {
			// retry, when error type cannot be detected
			log.V(1).Info("[DEBUG] failed to decode a neutron error", "error", e)
			return true
		}
		if neutronError.Type == "IpAddressGenerationFailure" {
			return true
		}

		// don't retry on quota or other errors
		return false
	case gophercloud.ResponseCodeIs(err, http.StatusBadRequest):
		neutronError, e := decodeNeutronError(err)
		if e != nil {
			// retry, when error type cannot be detected
			log.V(1).Info("[DEBUG] failed to decode a neutron error", "error", e)
			return true
		}
		if neutronError.Type == "ExternalIpAddressExhausted" {
			return true
		}

		// don't retry on quota or other errors
		return false
	case gophercloud.ResponseCodeIs(err, http.StatusNotFound):
		return true
	}

	return false
}

func decodeNeutronError(err error) (*neutronError, error) {
	var codeError gophercloud.ErrUnexpectedResponseCode
	if errors.As(err, &codeError) {
		e := &neutronErrorWrap{}
		if err := json.Unmarshal(codeError.Body, e); err != nil {
			return nil, err
		}
		return &e.NeutronError, nil
	}
	return nil, fmt.Errorf("not a unexpected gophercloud error")
}
