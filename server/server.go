/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"golang.org/x/sys/unix"
	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/jwt"
	"stash.kopano.io/kgol/ksurveyclient-go/autosurvey"

	"stash.kopano.io/kgol/kustomer"
	"stash.kopano.io/kgol/kustomer/license"
	"stash.kopano.io/kgol/kustomer/version"
)

const (
	defaultHTTPTimeout               = 30 * time.Second
	defaultHTTPKeepAlive             = 30 * time.Second
	defaultHTTPMaxIdleConns          = 5
	defaultHTTPIdleConnTimeout       = 5 * time.Second
	defaultHTTPTLSHandshakeTimeout   = 10 * time.Second
	defaultHTTPExpectContinueTimeout = 1 * time.Second

	offlineThreshold uint = 3
)

var DefaultHTTPUserAgent = "kustomerd/" + version.Version

// Server is our HTTP server implementation.
type Server struct {
	mutex sync.RWMutex

	config *Config

	logger      logrus.FieldLogger
	licensePath string
	listenPath  string
	sub         string

	insecure bool
	trusted  bool

	offline          uint
	offlineThreshold uint

	jwksURIs []*url.URL
	jwks     *jose.JSONWebKeySet
	certPool *x509.CertPool

	httpClient *http.Client

	readyCh  chan struct{}
	reloadCh chan chan struct{}
	updateCh chan struct{}
	closeCh  chan struct{}
	claims   []*license.Claims
}

// NewServer constructs a server from the provided parameters.
func NewServer(c *Config) (*Server, error) {
	s := &Server{
		config: c,
		logger: c.Logger,

		insecure: c.Insecure,
		trusted:  c.Trusted,

		offline:          offlineThreshold,
		offlineThreshold: offlineThreshold,

		jwksURIs: c.JWKSURIs,
		certPool: c.CertPool,

		readyCh:  make(chan struct{}),
		reloadCh: make(chan chan struct{}),
		updateCh: make(chan struct{}),
		closeCh:  make(chan struct{}),
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

	s.httpClient = func() *http.Client {
		transport := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   defaultHTTPTimeout,
				KeepAlive: defaultHTTPKeepAlive,
				DualStack: true,
			}).DialContext,
			MaxIdleConns:          defaultHTTPMaxIdleConns,
			IdleConnTimeout:       defaultHTTPIdleConnTimeout,
			TLSHandshakeTimeout:   defaultHTTPTLSHandshakeTimeout,
			ExpectContinueTimeout: defaultHTTPExpectContinueTimeout,
		}
		transport.TLSClientConfig = &tls.Config{
			ClientSessionCache: tls.NewLRUClientSessionCache(0),
			InsecureSkipVerify: c.Insecure,
		}
		err := http2.ConfigureTransport(transport)
		if err != nil {
			panic(err)
		}

		return &http.Client{
			Timeout:   defaultHTTPTimeout,
			Transport: transport,
		}
	}()
	if s.insecure {
		s.logger.Warnln("insecure mode, TLS client connections are susceptible to man-in-the-middle attacks")
	}

	return s, nil
}

// AddRoutes add the associated Servers URL routes to the provided router with
// the provided context.Context.
func (s *Server) AddRoutes(ctx context.Context, router *mux.Router) {
	// TODO(longsleep): Add subpath support to all handlers and paths.
	router.HandleFunc("/health-check", s.HealthCheckHandler)
	router.HandleFunc("/reload", s.ReloadHandler)
	router.HandleFunc("/api/v1/claims-gen", s.ClaimsGenHandler)
	router.HandleFunc("/api/v1/claims", s.ClaimsHandler)
	router.HandleFunc("/api/v1/claims/kopano/products", s.ClaimsKopanoProductsHandler)
	router.HandleFunc("/api/v1/claims/watch", s.MakeClaimsWatchHandler())
}

// Serve starts all the accociated servers resources and listeners and blocks
// forever until signals or error occurs.
func (s *Server) Serve(ctx context.Context) error {
	var err error

	serveCtx, serveCtxCancel := context.WithCancel(ctx)
	defer serveCtxCancel()

	logger := s.logger

	errCh := make(chan error, 2)
	exitCh := make(chan struct{}, 1)
	signalCh := make(chan os.Signal, 1)
	readyCh := make(chan struct{}, 1)
	triggerCh := make(chan bool, 1)

	// Check if listen socket can be created.
	err = func() error {
		_, statErr := os.Stat(s.listenPath)
		switch {
		case statErr == nil:
			// File exists, this might be a problem, so check if it is a socket
			// if something is listening on it.
			conn, connErr := net.DialTimeout("unix", s.listenPath, 5*time.Second)
			if connErr == nil {
				conn.Close()
				return fmt.Errorf("listen-path is already in use, refusing to replace")
			}
			if strings.Contains(connErr.Error(), "connection refused") {
				// Create our own lock, to avoid to delete twice.
				lock := s.listenPath + ".lock"
				f, _ := os.Create(lock)
				if lockErr := unix.Flock(int(f.Fd()), unix.LOCK_EX); lockErr != nil {
					return fmt.Errorf("failed to lock listen-path: %w", lockErr)
				}
				logger.WithField("socket", s.listenPath).Infoln("cleaning up unused existing listen-path")
				os.Remove(lock)
				os.Remove(s.listenPath)
				if lockErr := unix.Flock(int(f.Fd()), unix.LOCK_UN); lockErr != nil {
					return fmt.Errorf("failed to unlock listen-path: %w", lockErr)
				}
			} else {
				return fmt.Errorf("listen-path exists with error: %w, refusing to replace", connErr)
			}
		case os.IsNotExist(statErr):
			// Not exist, this is what we want. If kustomerd is run correctly via
			// systemd this is always the case since the runtime directory is
			// controlled by RuntimeDirectory.
		default:
			logger.WithError(statErr).Warnln("failed to access listen-path")
		}

		return nil
	}()
	if err != nil {
		return err
	}
	logger.WithField("socket", s.listenPath).Infoln("starting http listener")
	// NOTE(longsleep): On Linux, connecting to a stream socket object requires
	// write permission on that socket. See https://man7.org/linux/man-pages/man7/unix.7.html
	// for reference.
	umask := unix.Umask(0111) //nolint:gocritic // Octal umask, rw for all.
	listener, err := net.Listen("unix", s.listenPath)
	unix.Umask(umask) // Restore previous umask.
	if err != nil {
		return err
	}

	router := mux.NewRouter()
	s.AddRoutes(serveCtx, router)

	srv := &http.Server{
		Handler:     router,
		ConnContext: s.handleConnectionContext,
		BaseContext: func(net.Listener) context.Context {
			return serveCtx
		},
	}
	srv.SetKeepAlivesEnabled(false)

	// Load JWKS if we have one.
	go func() {
		if len(s.jwksURIs) == 0 {
			logger.Warnln("no JWKS URIs are set, running in offline mode")
			close(readyCh)
			return
		}

		var started bool
		var offline uint
		fetcher := kustomer.JWKSFetcher{
			URIs:      s.jwksURIs,
			UserAgent: DefaultHTTPUserAgent,

			Client: s.httpClient,
			Logger: logger,

			MaxRetries: 3,
		}
		for {
			jwks, requestErr := fetcher.Update(serveCtx)
			s.mutex.Lock()
			if requestErr != nil {
				logger.WithError(requestErr).Warnln("unable to fetch JWKS")
			} else if jwks != nil {
				logger.WithField("keys", len(jwks.Keys)).Debugln("JWKS loaded successfully")
				s.jwks = jwks
				if started {
					triggerCh <- true
				}
			}
			offline = s.offline
			if o := fetcher.Offline(); o {
				offline++
				if offline > s.offlineThreshold {
					offline = s.offlineThreshold
				}
			} else {
				offline = 0
			}
			if s.offline != offline {
				s.offline = offline
				if started {
					if offline > 0 {
						if offline > s.offlineThreshold {
							logger.Warnln("now offline")
						} else {
							logger.Debugln("now offline (threshold not reached yet)")
						}
					} else {
						logger.Warnln("no longer offline")
					}
				}
			}
			s.mutex.Unlock()
			if !started {
				close(readyCh)
				started = true
				if offline > 0 {
					logger.Warnln("started in offline mode, no JWKS is loaded")
				}
			}
			select {
			case <-serveCtx.Done():
				return
			case <-time.After(60 * time.Minute):
				// Refresh.
			}
		}
	}()

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
			s.mutex.RLock()
			updateCh := s.updateCh
			s.mutex.RUnlock()
			select {
			case <-serveCtx.Done():
				if cancel != nil {
					cancel()
				}
				return
			case <-updateCh:
				s.mutex.RLock()
				claims := s.claims
				s.mutex.RUnlock()
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
		loadHistory := make(map[string]*license.Claims)
		activateHistory := make(map[string]*license.Claims)
		var lastSub string
		var first bool = true
		var jwks *jose.JSONWebKeySet
		var offline bool
		f := func() {
			s.mutex.RLock()
			if jwks != s.jwks {
				jwks = s.jwks
				loadHistory = make(map[string]*license.Claims)
			}
			offline = s.offline > 0
			s.mutex.RUnlock()

			var sub string
			var claims []*license.Claims
			var changed bool
			// Load and parse license files.
			if s.licensePath != "" {
				scanner := &kustomer.LicensesLoader{
					CertPool: s.certPool,

					JWKS:    jwks,
					Offline: offline,

					Logger: logger,

					LoadHistory:     loadHistory,
					ActivateHistory: activateHistory,

					OnActivate: func(c *license.Claims) {
						products := []string{}
						for k := range c.Kopano.Products {
							products = append(products, k)
						}
						logger.WithFields(logrus.Fields{
							"name":     c.LicenseFileName,
							"products": products,
							"id":       c.Claims.ID,
							"customer": c.Claims.Subject,
						}).Infoln("licensed products activated")

					},
					OnRemove: func(c *license.Claims) {
						logger.WithField("id", c.LicenseID).Debugln("removed, triggering")
						changed = true
					},
					OnNew: func(c *license.Claims) {
						logger.WithField("id", c.LicenseID).Debugln("new, triggering")
						changed = true
					},
					OnSkip: func(c *license.Claims) {
						logger.WithField("name", c.LicenseFileName).Infoln("skipped")
					},
				}
				var scanErr error
				claims, scanErr = scanner.ScanFolder(s.licensePath, jwt.Expected{
					Time: time.Now(),
				})
				if scanErr != nil {
					logger.WithError(scanErr).Errorln("failed to scan for licenses")
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

			// Update active claims.
			s.mutex.Lock()
			s.claims = claims
			updateCh := s.updateCh
			s.updateCh = make(chan struct{})
			s.mutex.Unlock()

			if first {
				close(s.readyCh)
				first = false
				if s.config.OnFirstClaims != nil {
					go s.config.OnFirstClaims(s)
				}
			}
			close(updateCh)
		}
		select {
		case <-serveCtx.Done():
			return
		case <-readyCh:
			select {
			case <-triggerCh:
			default:
			}
			f()
		}
		for {
			select {
			case <-serveCtx.Done():
				return
			case cbCh := <-s.reloadCh:
				select {
				case <-triggerCh:
				default:
				}
				logger.Infoln("reload requested, scanning licenses")
				f()
				close(cbCh)
			case <-triggerCh:
				f()
			case <-time.After(60 * time.Second):
				select {
				case <-triggerCh:
				default:
				}
				f()
			}
		}
	}()

	go func() {
		select {
		case <-serveCtx.Done():
			return
		case <-readyCh:
		}
		s.mutex.RLock()
		offline := s.offline
		s.mutex.RUnlock()
		logger.WithFields(logrus.Fields{
			"insecure": s.insecure,
			"trusted":  s.trusted,
			"offline":  offline,
		}).Infoln("ready")
	}()

	// Wait for exit or error, with support for HUP to reload
	err = func() error {
		signal.Notify(signalCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
		for {
			select {
			case errFromChannel := <-errCh:
				return errFromChannel
			case reason := <-signalCh:
				if reason == syscall.SIGHUP {
					logger.Infoln("reload signal received, scanning licenses")
					select {
					case triggerCh <- true:
					default:
					}
					continue
				}
				logger.WithField("signal", reason).Warnln("received signal")
				return nil
			}
		}
	}()

	close(s.closeCh)

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
