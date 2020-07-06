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

	"github.com/google/uuid"

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
	err = encoder.Encode(claims)
	if err != nil {
		s.logger.WithError(err).Errorln("claims-gen failed to encode JSON")
	}
}
