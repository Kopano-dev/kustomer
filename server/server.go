/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/jwt"
	"stash.kopano.io/kgol/ksurveyclient-go/autosurvey"

	"stash.kopano.io/kgol/kustomer/license"
	"stash.kopano.io/kgol/kustomer/version"
)

const (
	licenseSizeLimitBytes = 1024 * 1024
	licenseLeeway         = 24 * time.Hour

	defaultHTTPTimeout               = 30 * time.Second
	defaultHTTPKeepAlive             = 30 * time.Second
	defaultHTTPMaxIdleConns          = 5
	defaultHTTPIdleConnTimeout       = 5 * time.Second
	defaultHTTPTLSHandshakeTimeout   = 10 * time.Second
	defaultHTTPExpectContinueTimeout = 1 * time.Second
)

var defaultHTTPUserAgent = "kustomerd/" + version.Version

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
	offline  bool

	jwksURI  *url.URL
	jwks     *jose.JSONWebKeySet
	certPool *x509.CertPool

	httpClient *http.Client

	readyCh chan struct{}
	claims  []*license.Claims
}

// NewServer constructs a server from the provided parameters.
func NewServer(c *Config) (*Server, error) {
	s := &Server{
		config: c,
		logger: c.Logger,

		insecure: c.Insecure,
		trusted:  c.Trusted,
		offline:  true,

		jwksURI:  c.JWKSURI,
		certPool: c.CertPool,

		readyCh: make(chan struct{}, 1),
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
	router.HandleFunc("/api/v1/claims-gen", s.ClaimsGenHandler)
	router.HandleFunc("/api/v1/claims", s.ClaimsHandler)
	router.HandleFunc("/api/v1/claims/kopano/products", s.ClaimsKopanoProductsHandler)
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
	updateCh := make(chan bool, 1)
	triggerCh := make(chan bool, 1)

	router := mux.NewRouter()
	s.AddRoutes(serveCtx, router)

	srv := &http.Server{
		Handler:     router,
		ConnContext: s.handleConnectionContext,
		BaseContext: func(net.Listener) context.Context {
			return serveCtx
		},
	}

	// Load JWKS if we have one.
	go func() {
		if s.jwksURI == nil {
			logger.Warnln("no JWKS URI is set, running in offline mode")
			close(readyCh)
			return
		}
		var started bool
		var etag string
		var offline = true
		requester := func() (*jose.JSONWebKeySet, error) {
			requestCtx, cancel := context.WithTimeout(serveCtx, 30*time.Second)
			defer cancel()
			request, requestErr := http.NewRequestWithContext(requestCtx, http.MethodGet, s.jwksURI.String(), nil)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Header.Set("User-Agent", defaultHTTPUserAgent)
			if etag != "" {
				request.Header.Set("If-None-Match", etag)
			}

			attempt := 1
			for {
				response, responseErr := s.httpClient.Do(request)
				if responseErr == nil {
					offline = false
					switch response.StatusCode {
					case http.StatusNotModified:
						// Nothing changed. Done for now.
						return nil, nil
					case http.StatusOK:
						etag = response.Header.Get("ETag")
						decoder := json.NewDecoder(response.Body)
						jwks := &jose.JSONWebKeySet{}
						decodeErr := decoder.Decode(jwks)
						response.Body.Close()
						if decodeErr == nil {
							logger.WithField("keys", len(jwks.Keys)).Debugln("JWKS loaded successfully")
							return jwks, nil
						} else {
							logger.WithError(decodeErr).Errorln("failed to parse JWKS")
						}
					default:
						logger.Errorln("unexpected response status when fetching JWKS: %d", response.StatusCode)
					}
				}
				offline = true
				if attempt >= 3 {
					return nil, responseErr
				}
				logger.WithError(responseErr).Debugln("error while fetching JWKS from URI (will retry)")
				select {
				case <-serveCtx.Done():
					return nil, nil
				case <-time.After(time.Duration(attempt) * 5 * time.Second):
					attempt++
				}
			}
		}

		for {
			jwks, requestErr := requester()
			s.mutex.Lock()
			if requestErr != nil {
				logger.WithError(requestErr).Warnln("unable to fetch JWKS")
			} else if jwks != nil {
				s.jwks = jwks
				if started {
					triggerCh <- true
				}
			}
			if s.offline != offline {
				s.offline = offline
				if started {
					if offline {
						logger.Warnln("now offline")
					} else {
						logger.Warnln("no longer offline")
					}
				}
			}
			s.mutex.Unlock()
			if !started {
				close(readyCh)
				started = true
				if offline {
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
		loadHistory := make(map[string]bool)
		activateHistory := make(map[string]bool)
		var lastSub string
		var first bool = true
		var jwks *jose.JSONWebKeySet
		var offline bool
		f := func() {
			s.mutex.RLock()
			if jwks != s.jwks {
				jwks = s.jwks
				loadHistory = make(map[string]bool)
			}
			offline = s.offline
			s.mutex.RUnlock()
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
							if _, ok := loadHistory[c.LicenseID]; ok {
								log = false
							}
							func() {
								r := io.LimitReader(f, licenseSizeLimitBytes)
								if raw, readErr := ioutil.ReadAll(r); readErr == nil {
									if token, parseErr := jwt.ParseSigned(string(raw)); parseErr == nil {
										if len(token.Headers) != 1 {
											if log {
												logger.WithField("name", fn).Warnln("license with multiple headers, ignored")
											}
											return
										}
										headers := token.Headers[0]
										switch jose.SignatureAlgorithm(headers.Algorithm) {
										case jose.EdDSA:
										case jose.ES256:
										case jose.ES384:
										case jose.ES512:
										default:
											if log {
												logger.WithFields(logrus.Fields{
													"alg":  headers.Algorithm,
													"name": fn,
												}).Warnln("license with unknown alg, ignored")
											}
											return
										}
										var key interface{}
										if jwks != nil {
											keys := jwks.Key(headers.KeyID)
											if len(keys) == 0 {
												if log {
													logger.WithFields(logrus.Fields{
														"kid":  headers.KeyID,
														"name": fn,
													}).Warnln("license with unknown kid, ignored")
												}
												return
											}
											key = &keys[0]
										}
										if key == nil {
											if !offline {
												if log {
													logger.WithFields(logrus.Fields{
														"kid":  headers.KeyID,
														"name": fn,
													}).Warnln("license found but there is no matching online key, skipped")
												}
												return
											}
											if s.certPool != nil {
												// If we have a certificate pool, try to validate the license with it
												// in offline mode.
												chain, certsErr := headers.Certificates(x509.VerifyOptions{
													Roots: s.certPool,
												})
												if certsErr != nil {
													if log {
														logger.WithError(certsErr).WithFields(logrus.Fields{
															"kid":  headers.KeyID,
															"name": fn,
														}).Warnln("license certificate check failed, skipped")
													}
													return
												}
												if len(chain) > 0 && len(chain[0]) > 0 {
													// Extract public key from chain.
													cert := chain[0][0]
													key = cert.PublicKey
												}
											}
											if key == nil {
												if log {
													logger.WithFields(logrus.Fields{
														"kid":  headers.KeyID,
														"name": fn,
													}).Warnln("license found but there is no matching offline key, skipped")
												}
												return
											}
										}
										if claimsErr := token.Claims(key, &c); claimsErr == nil {
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
							}()
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
			s.mutex.Lock()
			s.claims = claims
			s.mutex.Unlock()
			if first {
				close(s.readyCh)
				first = false
			}
			updateCh <- true
		}
		select {
		case <-serveCtx.Done():
			return
		case <-readyCh:
			f()
		}
		for {
			select {
			case <-serveCtx.Done():
				return
			case <-triggerCh:
				f()
			case <-time.After(60 * time.Second):
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
