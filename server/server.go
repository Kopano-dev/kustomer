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
	"syscall"

	"github.com/sirupsen/logrus"
	"stash.kopano.io/kgol/ksurveyclient-go/autosurvey"

	"stash.kopano.io/kgol/kustomer/version"
)

// Server is our HTTP server implementation.
type Server struct {
	email string

	logger logrus.FieldLogger
}

// NewServer constructs a server from the provided parameters.
func NewServer(c *Config) (*Server, error) {
	s := &Server{
		email: c.Email,

		logger: c.Logger,
	}

	return s, nil
}

// Serve starts all the accociated servers resources and listeners and blocks
// forever until signals or error occurs.
func (s *Server) Serve(ctx context.Context) error {
	var err error

	_, serveCtxCancel := context.WithCancel(ctx)
	defer serveCtxCancel()

	logger := s.logger

	errCh := make(chan error, 2)
	signalCh := make(chan os.Signal, 1)
	startCh := make(chan string)

	// Retporting via survey client.
	go func() {
		<-startCh
		logger.WithField("email", s.email).Infof("starting")
		err = autosurvey.Start(ctx, "kustomerd", version.Version, []byte(s.email))
		if err != nil {
			errCh <- fmt.Errorf("failed to start client: %v", err)
		}
	}()

	// TODO(longsleep): Load and parse license files here.

	logger.Debugln("ready")

	// Find email.
	if s.email != "" {
		close(startCh)
	} else {
		logger.Infoln("no customer information available, standing by")
	}

	// Wait for exit or error.
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err = <-errCh:
		// breaks
	case reason := <-signalCh:
		logger.WithField("signal", reason).Warnln("received signal")
		// breaks
	}

	// Cancel our own context,
	serveCtxCancel()

	return err
}
