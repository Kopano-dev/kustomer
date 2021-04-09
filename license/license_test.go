/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2021 Kopano and its licensors
 */

package license

import "testing"

func TestClaimsKopanoGetByName(t *testing.T) {
	// This test ensurs that the Kopano product claims in Claims struct can be
	// used properly without any data in it, so its not required to check for
	// nil all the time.
	c := &Claims{}

	if c.Kopano.Version != 0 {
		t.Errorf("unexpected version value: %d", c.Kopano.Version)
	}

	product, ok := c.Kopano.Products["test"]
	if ok || product != nil {
		t.Errorf("unexpected return value for non existing product")
	}

	// Just to be sure that it works in general, check an example product too.
	c.Kopano.Products = map[string]*Product{
		"example": {
			LicenseID: "example_id",
		},
	}
	product, ok = c.Kopano.Products["example"]
	if !ok || product.LicenseID != "example_id" {
		t.Errorf("unexpected return value for existing product")
	}
}
