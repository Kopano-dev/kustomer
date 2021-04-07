/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package license

import (
	"encoding/json"

	"gopkg.in/square/go-jose.v2/jwt"
)

// ExclusiveClaim is the claim used to describe which other claims are to be
// treated exclusive.
const ExclusiveClaim = "exclusive"

// A Product holds the license information for an individual Kopano product.
type Product struct {
	LicenseID string `json:"lid"`
	Unknown   map[string]interface{}
}

func (f *Product) UnmarshalJSON(data []byte) error {
	f.Unknown = make(map[string]interface{})
	err := json.Unmarshal(data, &f.Unknown)
	if err != nil {
		return err
	}
	lid, _ := f.Unknown["lid"].(string)
	f.LicenseID = lid
	delete(f.Unknown, "lid")
	return nil
}

func (f *Product) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{})
	for k, v := range f.Unknown {
		out[k] = v
	}
	out["lid"] = f.LicenseID
	return json.Marshal(out)
}

// A ProductSet is a mapping of keys for each product.
type ProductSet map[string]*Product

// Kopano is a container for Kopano product license information.
type Kopano struct {
	Version  int        `json:"v"`
	Products ProductSet `json:"products"`
}

// Claims are the claims for Kopano licenses.
type Claims struct {
	*jwt.Claims

	LicenseFileName string `json:"-"`
	LicenseID       string `json:"-"`
	Raw             []byte `json:"-"`

	LicenseFileID               string `json:"uid"`
	DisplayName                 string `json:"dn"`
	SupportIdentificationNumber string `json:"sin"`
	Kopano                      Kopano `json:"k"`
}
