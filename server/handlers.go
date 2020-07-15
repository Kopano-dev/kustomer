/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/longsleep/sse"
	"github.com/sirupsen/logrus"
	"gopkg.in/square/go-jose.v2/jwt"

	"stash.kopano.io/kgol/kustomer/license"
	api "stash.kopano.io/kgol/kustomer/server/api-v1"
)

// HealthCheckHandler a http handler return 200 OK when server health is fine.
func (s *Server) HealthCheckHandler(rw http.ResponseWriter, req *http.Request) {
	rw.WriteHeader(http.StatusOK)
}

// ReloadHandler is a http handler which triggers reloading of license files and
// returns when complete.
func (s *Server) ReloadHandler(rw http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		http.Error(rw, "failed to parse request form data", http.StatusBadRequest)
		return
	}

	if req.Method != http.MethodPost {
		http.Error(rw, "POST request required", http.StatusBadRequest)
		return
	}

	ucred, _ := GetUcredContextValue(req.Context())
	if ucred == nil {
		http.Error(rw, "no unix credentials in request", http.StatusInternalServerError)
		return
	}

	fields := logrus.Fields{
		"remote_uid":  ucred.Uid,
		"ua":          req.Header.Get("User-Agent"),
		"remote_addr": req.RemoteAddr,
	}
	if ucred.Uid != 0 {
		s.logger.WithFields(fields).Debugln("rejected reload request")
		http.Error(rw, "reload request must be sent as root", http.StatusForbidden)
		return
	}
	s.logger.WithFields(fields).Infoln("received reload request")

	// Trigger reload with callback channel.
	cbCh := make(chan struct{})
	select {
	case s.reloadCh <- cbCh:
		// breaks
	case <-req.Context().Done():
		return
	case <-time.After(30 * time.Second):
		err := fmt.Errorf("timeout triggering reload")
		s.logger.Errorln(err.Error())
		http.Error(rw, err.Error(), http.StatusInternalServerError)
		return
	}
	// Wait on callback.
	select {
	case <-req.Context().Done():
		return
	case <-cbCh:
		s.logger.Debugln("reload request complete")
		rw.WriteHeader(http.StatusOK)
	}
}

// ClaimsGenHandler is a http handler which can be used to generate license
// claims using simple URL form requests.
func (s *Server) ClaimsGenHandler(rw http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		http.Error(rw, "failed to parse request form data", http.StatusBadRequest)
		return
	}

	claims, err := license.GenerateClaims(req.Form)
	if err != nil {
		s.logger.WithError(err).Errorln("failed to generate claims")
		http.Error(rw, err.Error(), http.StatusBadRequest)
		return
	}

	rw.Header().Set("Content-Type", "application/json")

	encoder := json.NewEncoder(rw)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(&api.ClaimsGenResponse{
		Claims: claims,
	})
	if err != nil {
		s.logger.WithField("request_path", req.URL.Path).WithError(err).Errorln("failed to encode JSON")
	}
}

// ClaimsHandler is the http hadnler to return the active claims.
func (s *Server) ClaimsHandler(rw http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		http.Error(rw, "failed to parse request form data", http.StatusBadRequest)
		return
	}

	s.mutex.RLock()
	claims := s.claims
	s.mutex.RUnlock()

	response := api.ClaimsResponse(claims)

	rw.Header().Set("Content-Type", "application/json")

	encoder := json.NewEncoder(rw)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(response)
	if err != nil {
		s.logger.WithField("request_path", req.URL.Path).WithError(err).Errorln("failed to encode JSON")
	}
}

// ClaimsKopanoProductsHandler is a http handler to return the Kopano product
// data from all currently active license claims as array.
func (s *Server) ClaimsKopanoProductsHandler(rw http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		http.Error(rw, "failed to parse request form data", http.StatusBadRequest)
		return
	}

	var productFilter map[string]bool
	if requestedProducts, ok := req.Form["product"]; ok {
		productFilter = make(map[string]bool)
		for _, name := range requestedProducts {
			productFilter[name] = true
		}
	}

	func() {
		fields := logrus.Fields{
			"products":    req.Form["product"],
			"ua":          req.Header.Get("User-Agent"),
			"remote_addr": req.RemoteAddr,
		}
		if ucred, ok := GetUcredContextValue(req.Context()); ok {
			fields["remote_pid"] = ucred.Pid
			fields["remote_uid"] = ucred.Uid
		}
		s.logger.WithFields(fields).Debugln("received claims kopano products request")
	}()

	// Delay answering this request when the server not ready yet. This is a
	// help for the clients, so they do not have to implement their own fast
	// retry logic.
	select {
	case <-s.readyCh:
	case <-req.Context().Done():
		return
	case <-time.After(30 * time.Second):
		s.logger.Warnln("timeout while waiting for server to become ready in claims kopano products request")
		http.Error(rw, "ready timeout reached", http.StatusServiceUnavailable)
		return
	}

	s.mutex.RLock()
	claims := s.claims
	trusted := s.trusted
	offline := s.offline
	s.mutex.RUnlock()

	response := &api.ClaimsKopanoProductsResponse{
		Trusted:  trusted,
		Offline:  offline,
		Products: make(map[string]*api.ClaimsKopanoProductsResponseProduct),
	}
	products := response.Products
	for _, claim := range claims {
		if claim.Kopano.Products == nil {
			continue
		}
		for name, product := range claim.Kopano.Products {
			if productFilter != nil {
				if ok := productFilter[name]; !ok {
					continue
				}
			}
			entry, ok := products[name]
			if !ok {
				entry = &api.ClaimsKopanoProductsResponseProduct{
					OK:          true,
					Claims:      make(map[string]interface{}),
					Expirations: make([]*jwt.NumericDate, 0),
				}
				products[name] = entry
			}
			entry.Expirations = append(entry.Expirations, claim.Expiry)
			for k, nextValue := range product.Unknown {
				// Claims are sorted from older to newer. Means if unmergable
				// duplicate claims are encountered, the newer one wins.
				if haveValue, have := entry.Claims[k]; !have {
					entry.Claims[k] = nextValue
					continue
				} else {
					switch tNextValue := nextValue.(type) {
					case int64:
						tHaveValue, good := haveValue.(int64)
						if good {
							entry.Claims[k] = tHaveValue + tNextValue
						} else {
							s.logger.WithField("product", name).Debugf("int64 type mismatch in claim %s, using newest", k)
							entry.Claims[k] = tNextValue
						}
					case float64:
						tHaveValue, good := haveValue.(float64)
						if good {
							entry.Claims[k] = tHaveValue + tNextValue
						} else {
							s.logger.WithField("product", name).Debugf("float64 type mismatch in claim %s, using newest", k)
							entry.Claims[k] = tNextValue

						}
					default:
						// All other types must match, otherwise a warning will
						// be logged, and newest is used.
						if nextValue != haveValue {
							s.logger.WithField("product", name).Debugf("mismatch in claim value %s, using newest", k)
							entry.Claims[k] = nextValue
						}
					}
				}
			}
		}
	}

	rw.Header().Set("Content-Type", "application/json")

	encoder := json.NewEncoder(rw)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(response)
	if err != nil {
		s.logger.WithField("request_path", req.URL.Path).WithError(err).Errorln("failed to encode JSON")
	}
}

// MakeClaimsWatchHandler return a http handler which returns claims related
// events as server sent events.
func (s *Server) MakeClaimsWatchHandler() http.HandlerFunc {
	upgrader := sse.Upgrader{}
	version := "20200714"

	return func(rw http.ResponseWriter, req *http.Request) {
		err := req.ParseForm()
		if err != nil {
			http.Error(rw, "failed to parse request form data", http.StatusBadRequest)
			return
		}

		conn, err := upgrader.Upgrade(rw, req)
		if err != nil {
			s.logger.WithError(err).Debugln("failed to upgrade claims watch request to sse")
			http.Error(rw, "failed to update request", http.StatusBadRequest)
			return
		}

		ucred, _ := GetUcredContextValue(req.Context())
		if ucred == nil {
			http.Error(rw, "no unix credentials in request", http.StatusInternalServerError)
			return
		}

		start := time.Now()

		fields := logrus.Fields{
			"products":    req.Form["product"],
			"remote_uid":  ucred.Uid,
			"remote_pid":  ucred.Pid,
			"ua":          req.Header.Get("User-Agent"),
			"remote_addr": req.RemoteAddr,
		}
		s.logger.WithFields(fields).Infoln("claims watch started")
		defer func() {
			s.logger.WithFields(fields).WithField("duration", time.Since(start)).Infoln("claims watch ended")
		}()

		// Send initial hello.
		err = conn.WriteStringEvent("hello", version)
		if err != nil {
			s.logger.WithError(err).Debugln("failed to write claims watch initial hello sse event")
			return
		}

		// Block until request is done, or other action worty to send event.
		for {
			err = nil
			s.mutex.RLock()
			updateCh := s.updateCh
			s.mutex.RUnlock()

			select {
			case <-s.closeCh:
				return
			case <-req.Context().Done():
				return
			case <-updateCh:
				err = conn.WriteStringEvent("claims-updated", "true")
			case <-time.After(60 * time.Second):
				err = conn.WriteStringEvent("hello", version)
			}

			if err != nil {
				s.logger.WithError(err).Debugln("failed to write claims watch sse event")
				return
			}
		}
	}
}
