// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package client

import (
	"context"
	"net/http"

	"github.com/gophercloud/gophercloud/v2"
	"github.com/gophercloud/gophercloud/v2/openstack/objectstorage/v1/containers"
	"github.com/gophercloud/gophercloud/v2/openstack/objectstorage/v1/objects"
	"github.com/gophercloud/gophercloud/v2/pagination"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NewStorageClientFromSecretRef retrieves the openstack client from specified by the secret reference.
func NewStorageClientFromSecretRef(ctx context.Context, c client.Client, secretRef corev1.SecretReference, region string) (Storage, error) {
	base, err := NewOpenStackClientFromSecretRef(ctx, c, secretRef, nil)
	if err != nil {
		return nil, err
	}

	return base.Storage(WithRegion(region))
}

// DeleteObjectsWithPrefix deletes the blob objects with the specific <prefix> from <container>. If it does not exist,
// no error is returned.
func (s *StorageClient) DeleteObjectsWithPrefix(ctx context.Context, container, prefix string) error {
	opts := &objects.ListOpts{
		Prefix: prefix,
	}
	// NOTE: Though there is options of bulk-delete with openstack API,
	// Gophercloud doesn't yet support the bulk delete and we are not sure whether the openstack setup has enabled
	// bulk delete support. So, here we will fetch the list of object and delete it one by one.
	// In  future if support is added to upstream, we could switch to it.

	// Retrieve a pager (i.e. a paginated collection)
	pager := objects.List(s.client, container, opts)

	return pager.EachPage(ctx, func(ctx context.Context, page pagination.Page) (bool, error) {
		objectList, err := objects.ExtractNames(page)
		if err != nil {
			return false, err
		}
		for _, object := range objectList {
			if err := s.deleteObjectIfExists(ctx, container, object); err != nil {
				return false, err
			}
		}
		return true, nil
	})
}

// deleteObjectIfExists deletes the openstack object with name <objectName> from <container>. If it does not exist,
// no error is returned.
func (s *StorageClient) deleteObjectIfExists(ctx context.Context, container, objectName string) error {
	result := objects.Delete(ctx, s.client, container, objectName, nil)
	if _, err := result.Extract(); err != nil {
		if !IsNotFoundError(err) {
			return err
		}
	}
	return nil
}

// CreateContainerIfNotExists creates the openstack blob container with name <container>. If it already exist,
// no error is returned.
func (s *StorageClient) CreateContainerIfNotExists(ctx context.Context, container string) error {
	result := containers.Create(ctx, s.client, container, nil)
	if _, err := result.Extract(); err != nil {
		// Note: Openstack swift doesn't return any error if container already exists.
		// So, no special handling added here.
		return err
	}
	return nil
}

// DeleteContainerIfExists deletes the openstack blob container with name <container>. If it does not exist,
// no error is returned.
func (s *StorageClient) DeleteContainerIfExists(ctx context.Context, container string) error {
	result := containers.Delete(ctx, s.client, container)
	if _, err := result.Extract(); err != nil {
		switch {
		case gophercloud.ResponseCodeIs(err, http.StatusNotFound):
			return nil
		case gophercloud.ResponseCodeIs(err, http.StatusConflict):
			if err := s.DeleteObjectsWithPrefix(ctx, container, ""); err != nil {
				return err
			}
			return s.DeleteContainerIfExists(ctx, container)
		default:
			return err
		}
	}
	return nil
}
