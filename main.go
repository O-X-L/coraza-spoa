// Copyright The OWASP Coraza contributors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"flag"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/rs/zerolog"

	"github.com/corazawaf/coraza-spoa/internal"
)

var configPath string
var globalLogger = zerolog.New(os.Stderr).With().Timestamp().Logger()

func main() {
	flag.StringVar(&configPath, "config", "", "configuration file")
	flag.Parse()

	if configPath == "" {
		globalLogger.Fatal().Msg("Configuration file is not set")
	}

	cfg, err := readConfig()
	if err != nil {
		globalLogger.Fatal().Err(err).Msg("Failed loading config")
	}

	logger, err := cfg.Log.newLogger()
	if err != nil {
		globalLogger.Fatal().Err(err).Msg("Failed creating global logger")
	}
	globalLogger = logger

	apps, err := cfg.newApplications()
	if err != nil {
		globalLogger.Fatal().Err(err).Msg("Failed creating applications")
	}

	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	network, address := cfg.networkAddressFromBind()
	l, err := (&net.ListenConfig{}).Listen(ctx, network, address)
	if err != nil {
		globalLogger.Fatal().Err(err).Msg("Failed opening socket")
	}

	a := &internal.Agent{
		Context:      ctx,
		Applications: apps,
		Logger:       globalLogger,
	}
	go func() {
		defer cancelFunc()

		globalLogger.Info().Msg("Starting coraza-spoa")
		if err := a.Serve(l); err != nil {
			globalLogger.Fatal().Err(err).Msg("listener closed")
		}
	}()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR1, syscall.SIGINT)
	for {
		sig := <-sigCh
		switch sig {
		case syscall.SIGTERM:
			globalLogger.Info().Msg("Received SIGTERM, shutting down...")
			// this return will run cancel() and close the server
			return
		case syscall.SIGINT:
			globalLogger.Info().Msg("Received SIGINT, shutting down...")
			return
		case syscall.SIGHUP:
			globalLogger.Info().Msg("Received SIGHUP, reloading configuration...")

			newCfg, err := readConfig()
			if err != nil {
				globalLogger.Error().Err(err).Msg("Error loading configuration, using old configuration")
				continue
			}

			if cfg.Log != newCfg.Log {
				newLogger, err := newCfg.Log.newLogger()
				if err != nil {
					globalLogger.Error().Err(err).Msg("Error creating new global logger, using old configuration")
					continue
				}
				globalLogger = newLogger
			}

			if cfg.Bind != newCfg.Bind {
				globalLogger.Error().Msg("Changing bind is not supported yet, using old configuration")
				continue
			}

			apps, err := newCfg.newApplications()
			if err != nil {
				globalLogger.Error().Err(err).Msg("Error applying configuration, using old configuration")
				continue
			}

			a.ReplaceApplications(apps)
			cfg = newCfg
		}
	}
}