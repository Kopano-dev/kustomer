/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/square/go-jose.v2/jwt"
	"stash.kopano.io/kgol/ksurveyclient-go/autosurvey"

	"stash.kopano.io/kgol/kustomer/license"
	"stash.kopano.io/kgol/kustomer/version"
)

const (
	licenseSizeLimitBytes = 1024 * 1024
	licenseLeeway         = 24 * time.Hour
)

// Server is our HTTP server implementation.
type Server struct {
	config *Config

	logger      logrus.FieldLogger
	licensePath string
	sub         string
}

// NewServer constructs a server from the provided parameters.
func NewServer(c *Config) (*Server, error) {
	s := &Server{
		config: c,
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
	startCh := make(chan []*license.Claims, 1)

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
			case claims := <-startCh:
				if cancel != nil {
					if len(claims) == 0 {
						logger.Infof("deactivating")
					}
					cancel()
				}
				if len(claims) > 0 {
					sub := claims[0].Claims.Subject
					logger.WithField("sub", sub).Infof("activating")
					surveyCtx, survecCtxCancel := context.WithCancel(serveCtx)
					cancel = survecCtxCancel
					collector, _ := NewCollector(s.config, claims)
					err = autosurvey.Start(surveyCtx, "kustomerd", version.Version, []byte(sub), collector)
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
		var lastSub string
		var first bool
		f := func() {
			// TODO(longsleep): Load and parse JWKS here.
			var sub string
			claims := make([]*license.Claims, 0)
			// Load and parse license files.
			if s.licensePath != "" {
				expected := jwt.Expected{
					Time: time.Now(),
				}
				if files, readDirErr := ioutil.ReadDir(s.licensePath); readDirErr == nil {
					for _, info := range files {
						if info.IsDir() {
							continue
						}
						fn := filepath.Join(s.licensePath, info.Name())
						if f, openErr := os.Open(fn); openErr == nil {
							r := io.LimitReader(f, licenseSizeLimitBytes)
							if raw, readErr := ioutil.ReadAll(r); readErr == nil {
								if token, parseErr := jwt.ParseSigned(string(raw)); parseErr == nil {
									c := license.Claims{
										LicenseFileName: fn,
									}
									if claimsErr := token.UnsafeClaimsWithoutVerification(&c); claimsErr == nil {
										if c.Claims.Subject != "" {
											if validateErr := c.Claims.ValidateWithLeeway(expected, licenseLeeway); validateErr != nil {
												logger.WithError(validateErr).WithField("name", fn).Debugln("license is not valid")
											}
											claims = append(claims, &c)
										}
									} else {
										logger.WithError(claimsErr).WithField("name", fn).Errorln("error while parsing license file claims")
									}
								} else {
									logger.WithError(parseErr).WithField("name", fn).Errorln("error while parsing license file")
								}
							} else {
								logger.WithError(readErr).WithField("name", fn).Errorln("error while reading license file")
							}
							f.Close()
						} else {
							logger.WithError(openErr).WithField("name", fn).Errorln("failed to read license file")
						}
					}
				} else {
					logger.WithError(readDirErr).Errorln("failed to read license folder")
				}
			}
			// Sort reverse to prepare for uid deduplication (newer shall win).
			sort.SliceStable(claims, func(i int, j int) bool {
				return claims[i].Claims.IssuedAt.Time().After(claims[j].Claims.IssuedAt.Time())
			})
			// Deduplicate uid, sorted from newer to older, means everything
			// which was seen already can be removed.
			claims = func(claims []*license.Claims) []*license.Claims {
				result := make([]*license.Claims, 0)
				seen := make(map[string]bool)
				for _, c := range claims {
					if !seen[c.LicenseFileID] {
						if c.LicenseFileID != "" {
							seen[c.LicenseFileID] = true
						}
						// Prepend to also reverse.
						result = append([]*license.Claims{c}, result...)
					}
				}
				return result
			}(claims)
			// Add global configured sub to beginning.
			if s.sub != "" {
				claims = append([]*license.Claims{&license.Claims{
					Claims: &jwt.Claims{
						Subject: s.sub,
					},
				}}, claims...)
			}
			// Find sub.
			if len(claims) > 0 {
				sub = claims[0].Claims.Subject
			}
			if !first && sub == lastSub {
				return
			}
			lastSub = sub
			first = false
			// Start.
			startCh <- claims
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
