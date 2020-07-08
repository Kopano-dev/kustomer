/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"stash.kopano.io/kgol/kustomer/license"
)

// HealthCheckHandler a http handler return 200 OK when server health is fine.
func (s *Server) HealthCheckHandler(rw http.ResponseWriter, req *http.Request) {
	rw.WriteHeader(http.StatusOK)
}

// ClaimsGenHandler is a http handler which can be used to generate license
// claims using simple URL form requests.
func (s *Server) ClaimsGenHandler(rw http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		http.Error(rw, "failed to parse request form data", http.StatusBadRequest)
		return
	}

	uid, err := uuid.NewRandom()
	if err != nil {
		s.logger.WithError(err).Errorln("failed to generate uid")
		http.Error(rw, "failed to generate uid", http.StatusInternalServerError)
		return
	}

	claims := license.Claims{
		LicenseFileID: uid.String(),
	}
	claims.Kopano = license.Kopano{
		Version:  0,
		Products: make(license.ProductSet),
	}

	for k, values := range req.Form {
		if len(values) > 1 {
			err = fmt.Errorf("multiple values for key %v", k)
			http.Error(rw, err.Error(), http.StatusBadRequest)
			return
		}
		v := values[0]
		switch k {
		case "uid":
			claims.LicenseFileID = v
		default:
			parts := strings.SplitN(k, ".", 2)
			if len(parts) != 2 {
				err = fmt.Errorf("unsupported format in key %v", k)
				http.Error(rw, err.Error(), http.StatusBadRequest)
				return
			}

			product, ok := claims.Kopano.Products[parts[0]]
			if !ok {
				lid, lidErr := uuid.NewRandom()
				if lidErr != nil {
					s.logger.WithError(lidErr).Errorln("failed to generate lid")
					http.Error(rw, "failed to generate lid", http.StatusInternalServerError)
					return
				}
				product = &license.Product{
					LicenseID: lid.String(),
					Unknown:   make(map[string]interface{}),
				}
				claims.Kopano.Products[parts[0]] = product
			}

			t := "string"
			kWithType := strings.SplitN(parts[1], ":", 2)
			k := kWithType[0]
			if len(kWithType) == 2 {
				t = kWithType[1]
			}

			switch k {
			case "lid":
				product.LicenseID = v
			case "":
				// Ignore.
			default:
				var tv interface{} = v
				switch t {
				case "string":
				case "int":
					iv, parseErr := strconv.ParseInt(v, 10, 64)
					if parseErr != nil {
						err = fmt.Errorf("failed to parse int value for %v: %w", k, parseErr)
						http.Error(rw, err.Error(), http.StatusInternalServerError)
						return
					}
					tv = iv
				case "bool":
					bv, parseErr := strconv.ParseBool(v)
					if parseErr != nil {
						err = fmt.Errorf("failed to parse bool value for %v: %w", k, parseErr)
						http.Error(rw, err.Error(), http.StatusInternalServerError)
						return
					}
					tv = bv
				case "float":
					fv, parseErr := strconv.ParseFloat(v, 64)
					if parseErr != nil {
						err = fmt.Errorf("failed to parse float value for %v: %w", k, parseErr)
						http.Error(rw, err.Error(), http.StatusInternalServerError)
						return
					}
					tv = fv
				}
				product.Unknown[k] = tv
			}
		}
	}

	rw.Header().Set("Content-Type", "application/json")

	encoder := json.NewEncoder(rw)
	encoder.SetIndent("", "  ")
	err = encoder.Encode(&ClaimsGenResponse{
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

	response := ClaimsResponse(claims)

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

	response := &ClaimsKopanoProductsResponse{
		Trusted:  trusted,
		Offline:  offline,
		Products: make(map[string]ClaimsKopanoProductsResponseProduct),
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
				entry = ClaimsKopanoProductsResponseProduct{
					OK:     true,
					Claims: make(map[string]interface{}),
				}
				products[name] = entry
			}
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
							s.logger.WithField("product", product).Debugf("int64 type mismatch in claim %s, using newest", k)
							entry.Claims[k] = tNextValue
						}
					case float64:
						tHaveValue, good := haveValue.(float64)
						if good {
							entry.Claims[k] = tHaveValue + tNextValue
						} else {
							s.logger.WithField("product", product).Debugf("float64 type mismatch in claim %s, using newest", k)
							entry.Claims[k] = tNextValue

						}
					default:
						// All other types must match, otherwise a warning will
						// be logged, and newest is used.
						if nextValue != haveValue {
							s.logger.WithField("product", product).Debugf("mismatch in claim value %s, using newest", k)
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
