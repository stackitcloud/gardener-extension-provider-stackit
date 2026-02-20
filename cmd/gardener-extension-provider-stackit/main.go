// SPDX-FileCopyrightText: 2024 SAP SE or an SAP affiliate company and Gardener contributors
//
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"

	"github.com/gardener/gardener/pkg/logger"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"

	"github.com/stackitcloud/gardener-extension-provider-stackit/cmd/gardener-extension-provider-stackit/app"
)

func main() {
	log.SetLogger(logger.MustNewZapLogger(logger.InfoLevel, logger.FormatJSON))
	setupLogger := log.Log.WithName("setup")

	cmd := app.NewControllerManagerCommand(signals.SetupSignalHandler())
	if err := cmd.Execute(); err != nil {
		setupLogger.Error(err, "error executing the main controller command")
		os.Exit(1)
	}
}
