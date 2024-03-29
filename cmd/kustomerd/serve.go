/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package main

import (
	"context"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	systemDaemon "github.com/coreos/go-systemd/v22/daemon"
	"github.com/spf13/cobra"
	"stash.kopano.io/kgol/ksurveyclient-go/autosurvey"

	"stash.kopano.io/kgol/kustomer/server"
)

var defaultCustomerClientSubmitURL = "https://kustomer.kopano.com/api/stats/v1/submit"

var defaultTrusted = true
var defaultInsecure = false
var defaultSystemdNotify = false

var globalSub = ""
var licensesPath = "/etc/kopano/licenses"
var listenPath = "/run/kopano-kustomerd/api.sock"

func init() {
	// Disable auto hashing of GUID values. We control this ourselves.
	autosurvey.AutoHashGUID = ""

	// Setup survey client for kustomer endpoints.
	autosurvey.DefaultConfig = autosurvey.DefaultConfig.Clone()
	if v := os.Getenv("KOPANO_CUSTOMERCLIENT_URL"); v != "" {
		defaultCustomerClientSubmitURL = v
		defaultTrusted = false
	}
	autosurvey.DefaultConfig.URL = defaultCustomerClientSubmitURL
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
		defaultTrusted = false
	}
	if v := os.Getenv("KOPANO_KUSTOMERD_LICENSE_JWKS_URI"); v != "" {
		server.DefaultLicenseJWKSURI = v
		defaultTrusted = false
	}
	if v := os.Getenv("KOPANO_KUSTOMERD_LICENSE_SUB"); v != "" {
		globalSub = strings.TrimSpace(v)
	}

	rand.Seed(time.Now().UnixNano())
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
	serveCmd.Flags().BoolVar(&defaultInsecure, "insecure", defaultInsecure, "Disable TLS certificate and hostname validation")
	serveCmd.Flags().BoolVar(&defaultSystemdNotify, "systemd-notify", defaultSystemdNotify, "Enable systemd sd_notify callback")

	return serveCmd
}

func serve(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	logTimestamp, _ := cmd.Flags().GetBool("log-timestamp")
	logLevel, _ := cmd.Flags().GetString("log-level")

	logger, err := newLogger(!logTimestamp, logLevel)
	if err != nil {
		return fmt.Errorf("failed to create logger: %w", err)
	}

	logger.Debugln("serve start")

	trusted := defaultTrusted

	certPool := x509.NewCertPool()
	if server.DefaultLicenseCertsBase64 != "" {
		licenseCerts, decodeErr := base64.StdEncoding.DecodeString(server.DefaultLicenseCertsBase64)
		if decodeErr != nil {
			return fmt.Errorf("failed to decode license root certificate: %w", decodeErr)
		}
		if certPool.AppendCertsFromPEM(licenseCerts) {
			logger.WithField("count", len(certPool.Subjects())).Infoln("loaded root license certificates")
		} else {
			logger.Warnln("no license root certificates loaded")
			trusted = false
		}
	} else {
		logger.Infoln("no license root certificates configured")
		trusted = false
	}

	jwksURIs := make([]*url.URL, 0)
	if server.DefaultLicenseJWKSURI != "" {
		jwksURIsExtra := make([]*url.URL, 0)
		for idx, jwksURIString := range strings.Split(server.DefaultLicenseJWKSURI, ",") {
			if jwksURI, parseErr := url.Parse(jwksURIString); parseErr != nil {
				return fmt.Errorf("failed to parse JWKS URI: %w", parseErr)
			} else {
				if idx == 0 {
					// Always go to main URI first.
					jwksURIs = append(jwksURIs, jwksURI)
				} else {
					jwksURIsExtra = append(jwksURIsExtra, jwksURI)
				}
			}
		}
		if len(jwksURIsExtra) > 0 {
			// Randomize backup URI order.
			rand.Shuffle(len(jwksURIsExtra), func(i, j int) {
				jwksURIsExtra[i], jwksURIsExtra[j] = jwksURIsExtra[j], jwksURIsExtra[i]
			})
			jwksURIs = append(jwksURIs, jwksURIsExtra...)
		}
		logger.WithField("jwks_uris", jwksURIs).Infoln("JWKS URIs available")
	} else {
		trusted = false
		logger.Warnln("no JWKS URIs set, this is odd - development build?")
	}

	if !trusted {
		logger.Warnln("customization detected, services might reject license information")
	}

	cfg := &server.Config{
		Sub: globalSub,

		LicensesPath: licensesPath,
		ListenPath:   listenPath,

		Insecure: defaultInsecure,

		Trusted:  trusted,
		JWKSURIs: jwksURIs,
		CertPool: certPool,

		Logger: logger,

		OnFirstClaims: func(srv *server.Server) {
			if defaultSystemdNotify {
				ok, notifyErr := systemDaemon.SdNotify(false, systemDaemon.SdNotifyReady)
				logger.WithField("ok", ok).Debugln("called systemd sd_notify ready")
				if notifyErr != nil {
					logger.WithError(notifyErr).Errorln("failed to trigger systemd sd_notify")
				}
			}
		},
	}

	srv, err := server.NewServer(cfg)
	if err != nil {
		return err
	}

	return srv.Serve(ctx)
}
