/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package api

import (
	"gopkg.in/square/go-jose.v2/jwt"

	"stash.kopano.io/kgol/kustomer/license"
)

// CLaimsGenResponse is the response model for the claims-gen API endpoint response.
type ClaimsGenResponse struct {
	*license.Claims
}

// ClaimsResponse is the response model for the claims API endpoint response.
type ClaimsResponse []*license.Claims

// ClaimsKopanoProductsResponse defines the response model of the claims kopano
// products API endpoint.
type ClaimsKopanoProductsResponse struct {
	Trusted  bool                                            `json:"trusted"`
	Offline  bool                                            `json:"offline"`
	Products map[string]*ClaimsKopanoProductsResponseProduct `json:"products"`
}

// ClaimsKopanoProductsResponseProduct is the individual product entryu for
// products returned by the kopano products API endpoint.
type ClaimsKopanoProductsResponseProduct struct {
	OK                          bool                   `json:"ok"`
	Claims                      map[string]interface{} `json:"claims"`
	Expiry                      []*jwt.NumericDate     `json:"expiry"`
	DisplayName                 []string               `json:"dn"`
	SupportIdentificationNumber []string               `json:"sin"`

	ExclusiveClaims map[string]interface{} `json:"-"`
}
