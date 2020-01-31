/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"stash.kopano.io/kgol/ksurveyclient-go/autosurvey"

	"stash.kopano.io/kgol/kustomer/version"
)

// Server is our HTTP server implementation.
type Server struct {
	sub         string
	licensePath string

	logger logrus.FieldLogger
}

// NewServer constructs a server from the provided parameters.
func NewServer(c *Config) (*Server, error) {
	s := &Server{
		logger: c.Logger,
	}

	if c.Sub != "" {
		// Check if provided sub is email.
		s.sub = strings.TrimSpace(c.Sub)
		if isValidEmail(s.sub) {
			s.sub = hashSub(s.sub)
		}
	}

	if c.LicensesPath != "" {
		// Validate license path
		licensePath, absErr := filepath.Abs(c.LicensesPath)
		if absErr != nil {
			return nil, fmt.Errorf("invalid license path: %w", absErr)
		}
		s.licensePath = licensePath
	}

	return s, nil
}

// Serve starts all the accociated servers resources and listeners and blocks
// forever until signals or error occurs.
func (s *Server) Serve(ctx context.Context) error {
	var err error

	serveCtx, serveCtxCancel := context.WithCancel(ctx)
	defer serveCtxCancel()

	logger := s.logger

	errCh := make(chan error, 2)
	signalCh := make(chan os.Signal, 1)
	startCh := make(chan string, 1)

	// Reporting via survey client.
	go func() {
		var cancel context.CancelFunc
		for {
			select {
			case <-serveCtx.Done():
				if cancel != nil {
					cancel()
				}
				return
			case sub := <-startCh:
				if cancel != nil {
					if sub == "" {
						logger.Infof("deactivating")
					}
					cancel()
				}
				if sub != "" {
					logger.WithField("sub", sub).Infof("activating")
					surveyCtx, survecCtxCancel := context.WithCancel(serveCtx)
					cancel = survecCtxCancel
					err = autosurvey.Start(surveyCtx, "kustomerd", version.Version, []byte(sub))
					if err != nil {
						errCh <- fmt.Errorf("failed to start client: %v", err)
					}
				} else {
					logger.Infoln("no customer information available, standing by")
				}
			}
		}
	}()

	// License loading / watching.
	go func() {
		ticker := time.NewTicker(time.Minute)
		var sub string
		var first bool
		f := func() {
			// TODO(longsleep): Load and parse license files here.
			// Find sub.
			if !first && s.sub == sub {
				return
			}
			sub = s.sub
			first = false
			startCh <- s.sub
		}
		select {
		case <-serveCtx.Done():
			return
		default:
			f()
		}
		for {
			select {
			case <-serveCtx.Done():
				return
			case <-ticker.C:
				f()
			}
		}

	}()

	logger.Debugln("ready")

	// Wait for exit or error.
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err = <-errCh:
		// breaks
	case reason := <-signalCh:
		logger.WithField("signal", reason).Warnln("received signal")
		// breaks
	}

	logger.Infoln("clean server shutdown")

	// Cancel our own context,
	serveCtxCancel()

	return err
}
