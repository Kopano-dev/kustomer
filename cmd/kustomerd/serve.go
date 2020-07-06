/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"stash.kopano.io/kgol/ksurveyclient-go/autosurvey"

	"stash.kopano.io/kgol/kustomer/server"
)

var defaultSubmitURL = "https://kustomer.kopano.com/api/stats/v1/submit"
var defaultJWKSURI = "https://kustomer.kopano.com/api/stats/v1/jwks.json"

var globalSub = ""
var licensesPath = "/etc/kopano/licenses"
var listenPath = "/run/kopano-kustomerd/api.sock"

func init() {
	// Disable auto hashing of GUID values. We control this ourselves.
	autosurvey.AutoHashGUID = ""

	// Setup survey client for kustomer endpoints.
	autosurvey.DefaultConfig = autosurvey.DefaultConfig.Clone()
	if v := os.Getenv("KOPANO_CUSTOMERCLIENT_URL"); v != "" {
		defaultSubmitURL = v
	}
	autosurvey.DefaultConfig.URL = defaultSubmitURL
	if v := os.Getenv("KOPANO_CUSTOMERCLIENT_JWKS"); v != "" {
		defaultJWKSURI = v
	}
	if v := os.Getenv("KOPANO_CUSTOMERCLIENT_START_DELAY"); v != "" {
		autosurvey.DefaultConfig.StartDelay, _ = strconv.ParseUint(v, 10, 64)
	}
	if v := os.Getenv("KOPANO_CUSTOMERCLIENT_ERROR_DELAY"); v != "" {
		autosurvey.DefaultConfig.ErrorDelay, _ = strconv.ParseUint(v, 10, 64)
	}
	if v := os.Getenv("KOPANO_CUSTOMERCLIENT_INTERVAL"); v != "" {
		autosurvey.DefaultConfig.Interval, _ = strconv.ParseUint(v, 10, 64)
	}
	if v := os.Getenv("KOPANO_CUSTOMERCLIENT_INSECURE"); v != "" {
		autosurvey.DefaultConfig.Insecure = v == "yes"
	}
	if v := os.Getenv("KOPANO_CUSTOMERCLIENT_SUB"); v != "" {
		globalSub = strings.TrimSpace(v)
	}
}

func commandServe() *cobra.Command {
	serveCmd := &cobra.Command{
		Use:   "serve [...args]",
		Short: "Start service",
		Run: func(cmd *cobra.Command, args []string) {
			if err := serve(cmd, args); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	serveCmd.Flags().Bool("log-timestamp", true, "Prefix each log line with timestamp")
	serveCmd.Flags().String("log-level", "info", "Log level (one of panic, fatal, error, warn, info or debug)")
	serveCmd.Flags().StringVar(&licensesPath, "licenses-path", licensesPath, "Path to the folder containing Kopano license files")
	serveCmd.Flags().StringVar(&listenPath, "listen-path", listenPath, "Path to unix socket for API requests")

	return serveCmd
}

func serve(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	logTimestamp, _ := cmd.Flags().GetBool("log-timestamp")
	logLevel, _ := cmd.Flags().GetString("log-level")

	logger, err := newLogger(!logTimestamp, logLevel)
	if err != nil {
		return fmt.Errorf("failed to create logger: %v", err)
	}
	logger.Debugln("serve start")

	cfg := &server.Config{
		Sub: globalSub,

		LicensesPath: licensesPath,
		ListenPath:   listenPath,

		JWKURI: defaultJWKSURI,

		Logger: logger,
	}

	srv, err := server.NewServer(cfg)
	if err != nil {
		return err
	}

	return srv.Serve(ctx)
}
