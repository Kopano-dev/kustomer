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
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/mux"
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
	listenPath  string
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
	if c.ListenPath != "" {
		// Validate listen path
		listenPath, absErr := filepath.Abs(c.ListenPath)
		if absErr != nil {
			return nil, fmt.Errorf("invalid listen path: %w", absErr)
		}
		s.listenPath = listenPath
	}

	return s, nil
}

// AddRoutes add the associated Servers URL routes to the provided router with
// the provided context.Context.
func (s *Server) AddRoutes(ctx context.Context, router *mux.Router) {
	// TODO(longsleep): Add subpath support to all handlers and paths.
	router.HandleFunc("/health-check", s.HealthCheckHandler)
	router.HandleFunc("/api/v1/claims-gen", s.ClaimsGenHandler)
}

// Serve starts all the accociated servers resources and listeners and blocks
// forever until signals or error occurs.
func (s *Server) Serve(ctx context.Context) error {
	var err error

	serveCtx, serveCtxCancel := context.WithCancel(ctx)
	defer serveCtxCancel()

	logger := s.logger

	errCh := make(chan error, 2)
	exitCh := make(chan bool, 1)
	signalCh := make(chan os.Signal, 1)
	startCh := make(chan []*license.Claims, 1)

	router := mux.NewRouter()
	s.AddRoutes(serveCtx, router)

	srv := &http.Server{
		Handler: router,
	}

	logger.WithField("socket", s.listenPath).Infoln("starting http listener")
	listener, err := net.Listen("unix", s.listenPath)
	if err != nil {
		return err
	}

	// HTTP listener.
	go func() {
		serveErr := srv.Serve(listener)
		if serveErr != nil {
			errCh <- serveErr
		}

		logger.Debugln("http listener stopped")
		close(exitCh)
	}()

	// Reporting via survey client.
	go func() {
		var cancel context.CancelFunc
		var collector *Collector
		for {
			select {
			case <-serveCtx.Done():
				if cancel != nil {
					cancel()
				}
				return
			case claims := <-startCh:
				if collector != nil {
					collector.setClaims(claims)
				} else {
					if len(claims) > 0 {
						sub := claims[0].Claims.Subject
						logger.WithFields(logrus.Fields{
							"sub":  sub,
							"name": claims[0].LicenseFileName,
						}).Infof("activating licensed services")
						surveyCtx, survecCtxCancel := context.WithCancel(serveCtx)
						cancel = survecCtxCancel
						collector, _ = NewCollector(s.config, claims)
						err = autosurvey.Start(surveyCtx, "kustomerd", version.Version, []byte(sub), collector)
						if err != nil {
							errCh <- fmt.Errorf("failed to start client: %v", err)
						}
					} else {
						logger.Infoln("no customer information available, standing by")
					}
				}
			}
		}
	}()

	// License loading / watching.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		loadHistory := make(map[string]bool)
		activateHistory := make(map[string]bool)
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
							log := true
							c := license.Claims{
								LicenseID:       fn,
								LicenseFileName: fn,
							}
							r := io.LimitReader(f, licenseSizeLimitBytes)
							if raw, readErr := ioutil.ReadAll(r); readErr == nil {
								if token, parseErr := jwt.ParseSigned(string(raw)); parseErr == nil {
									if claimsErr := token.UnsafeClaimsWithoutVerification(&c); claimsErr == nil {
										if c.Claims.ID != "" {
											c.LicenseID = c.Claims.ID
										}
										if _, ok := loadHistory[c.LicenseID]; ok {
											log = false
										}
										if c.Claims.Subject != "" {
											if validateErr := c.Claims.ValidateWithLeeway(expected, licenseLeeway); validateErr != nil {
												if log {
													logger.WithError(validateErr).WithField("name", fn).Warnln("license is not valid, skipped")
												}
											} else {
												claims = append(claims, &c)
												if log {
													logger.WithField("name", fn).Debugln("license is valid, loaded")
												}
											}
										}
									} else {
										if log {
											logger.WithError(claimsErr).WithField("name", fn).Errorln("error while parsing license file claims")
										}
									}
								} else {
									if log {
										logger.WithError(parseErr).WithField("name", fn).Errorln("error while parsing license file")
									}
								}
							} else {
								logger.WithError(readErr).WithField("name", fn).Errorln("error while reading license file")
							}
							f.Close()
							if log {
								loadHistory[c.LicenseID] = true
							}
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
			added := make(map[string]bool)
			claims = func(claims []*license.Claims) []*license.Claims {
				result := make([]*license.Claims, 0)
				seen := make(map[string]bool)
				for _, c := range claims {
					log := true
					if _, ok := activateHistory[c.LicenseID]; ok {
						added[c.LicenseID] = false
						log = false
					} else {
						added[c.LicenseID] = true
						activateHistory[c.LicenseID] = true
					}
					if !seen[c.LicenseFileID] {
						if c.LicenseFileID != "" {
							seen[c.LicenseFileID] = true
						}
						// Prepend to also reverse.
						result = append([]*license.Claims{c}, result...)
						products := []string{}
						for k := range c.Kopano.Products {
							products = append(products, k)
						}
						if log {
							logger.WithFields(logrus.Fields{
								"name":     c.LicenseFileName,
								"products": products,
								"id":       c.Claims.ID,
							}).Infoln("licensed products activated")
						}
					} else {
						if log {
							logger.WithField("name", c.LicenseFileName).Infoln("skipped")
						}
					}
				}
				return result
			}(claims)
			changed := false
			for k := range activateHistory {
				if _, ok := added[k]; !ok {
					delete(activateHistory, k)
					logger.WithField("id", k).Debugln("removed, triggering")
					changed = true
				}
			}
			for k, v := range added {
				if v {
					logger.WithField("id", k).Debugln("new, triggering")
					changed = true
				}
			}
			// Add global configured sub to beginning.
			if s.sub != "" {
				if changed {
					logger.WithField("sub", s.sub).Debugln("using global configured sub")
				}
				claims = append([]*license.Claims{{
					Claims: &jwt.Claims{
						Subject: s.sub,
					},
				}}, claims...)
			}
			// Find sub.
			if len(claims) > 0 {
				sub = claims[0].Claims.Subject
			}
			if !first && sub == lastSub && !changed {
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

	logger.Infoln("ready")

	// Wait for exit or error.
	signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case err = <-errCh:
		// breaks
	case reason := <-signalCh:
		logger.WithField("signal", reason).Warnln("received signal")
		// breaks
	}

	// Shutdown, server will stop to accept new connections, requires Go 1.8+.
	logger.Infoln("clean server shutdown start")
	shutDownCtx, shutDownCtxCancel := context.WithTimeout(ctx, 10*time.Second)
	if shutdownErr := srv.Shutdown(shutDownCtx); shutdownErr != nil {
		logger.WithError(shutdownErr).Warn("clean server shutdown failed")
	}

	// Cancel our own context,
	serveCtxCancel()
	func() {
		for {
			select {
			case <-exitCh:
				return
			default:
				// HTTP listener has not quit yet.
				logger.Info("waiting for http listener to exit")
			}
			select {
			case reason := <-signalCh:
				logger.WithField("signal", reason).Warn("received signal")
				return
			case <-time.After(100 * time.Millisecond):
			}
		}
	}()
	shutDownCtxCancel() // prevent leak.

	return err
}
