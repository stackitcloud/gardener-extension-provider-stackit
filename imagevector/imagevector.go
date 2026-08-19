// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package imagevector

import (
	_ "embed"

	"github.com/gardener/gardener/pkg/utils/imagevector"
	"k8s.io/apimachinery/pkg/util/runtime"
)

var (
	// ImagesYAML contains the content of the images.yaml file
	//go:embed images.yaml
	imagesYAML string
	// ImageVector is the image vector that contains all the needed images.
	imageVector imagevector.ImageVector
	// caBundle contains the optional registry CA bundle from the image vector overwrite.
	caBundle *imagevector.CABundle
)

func init() {
	var err error

	imageVector, caBundle, err = imagevector.Read([]byte(imagesYAML))
	runtime.Must(err)

	imageVector, caBundle, err = imagevector.WithEnvOverride(imageVector, caBundle, imagevector.OverrideEnv)
	runtime.Must(err)
}

// ImageVector is the image vector that contains all the needed images.
func ImageVector() imagevector.ImageVector {
	return imageVector
}

// CABundle contains the optional registry CA bundle from the image vector configuration.
func CABundle() *imagevector.CABundle {
	return caBundle
}
