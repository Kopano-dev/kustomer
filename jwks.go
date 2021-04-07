/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package kustomer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/square/go-jose.v2"
)

// A JWKSFetcher defines the parameters how to fetch a JWK set from URI.
type JWKSFetcher struct {
	URIs      []*url.URL
	UserAgent string

	Client *http.Client
	Logger logrus.FieldLogger

	MaxRetries int

	jwks    *jose.JSONWebKeySet
	etag    string
	offline bool
}

// Update fetches the JWKS from its URI with retry.
func (jwksf *JWKSFetcher) Update(ctx context.Context) (*jose.JSONWebKeySet, error) {
	logger := jwksf.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	var attempt int = 1
	var uriIndex int
	for {
		uriIndex = attempt - 1
		if uriIndex >= len(jwksf.URIs) {
			uriIndex = 0
		}
		jwks, etag, err := func(uri *url.URL, userAgent string, etag string) (*jose.JSONWebKeySet, string, error) {
			requestCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			request, requestErr := http.NewRequestWithContext(requestCtx, http.MethodGet, uri.String(), nil)
			if requestErr != nil {
				return nil, "", requestErr
			}
			if userAgent != "" {
				request.Header.Set("User-Agent", userAgent)
			}
			if etag != "" {
				request.Header.Set("If-None-Match", etag)
			}

			response, responseErr := jwksf.Client.Do(request)
			if responseErr != nil {
				return nil, "", responseErr
			}

			switch response.StatusCode {
			case http.StatusNotModified:
				// Nothing changed. Done for now.
				return nil, etag, nil
			case http.StatusOK:
				etag = response.Header.Get("ETag")
				decoder := json.NewDecoder(response.Body)
				jwks := &jose.JSONWebKeySet{}
				decodeErr := decoder.Decode(jwks)
				response.Body.Close()
				if decodeErr == nil {
					logger.WithField("keys", len(jwks.Keys)).Debugln("JWKS loaded successfully")
					return jwks, etag, nil
				} else {
					return nil, etag, fmt.Errorf("failed to parse JWKS from %s: %w", jwksf.URIs[uriIndex], decodeErr)
				}
			default:
				return nil, etag, fmt.Errorf("unexpected response status %d when fetching JWKS from %s", response.StatusCode, jwksf.URIs[uriIndex])
			}
		}(jwksf.URIs[uriIndex], jwksf.UserAgent, jwksf.etag)
		if err == nil {
			jwksf.offline = false
			if jwks != nil {
				jwksf.jwks = jwks
				jwksf.etag = etag
			}
			return jwks, nil
		}

		jwksf.offline = true
		if attempt >= jwksf.MaxRetries {
			logger.WithError(err).Errorln("failed to fetch JWKS from URI")
			return nil, err
		}
		logger.WithError(err).Infoln("error while fetching JWKS from URI (will retry)")
		select {
		case <-ctx.Done():
			return nil, nil
		case <-time.After(time.Duration(attempt) * 5 * time.Second):
			attempt++
		}
	}
}

func (jwksf *JWKSFetcher) Offline() bool {
	return jwksf.offline
}

func (jwksf *JWKSFetcher) JWKS() *jose.JSONWebKeySet {
	return jwksf.jwks
}

func (jwksf *JWKSFetcher) ETag() string {
	return jwksf.etag
}
