/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/square/go-jose.v2/jwt"
	"stash.kopano.io/kgol/ksurveyclient-go"

	"stash.kopano.io/kgol/kustomer/license"
)

type Collector struct {
	claims []*license.Claims
	logger logrus.FieldLogger
}

func NewCollector(c *Config, claims []*license.Claims) (*Collector, error) {
	return &Collector{
		claims: claims,
		logger: c.Logger,
	}, nil
}

func (c *Collector) Collect(ch chan<- ksurveyclient.Metric) {
	licenseCustomer := make([]string, 0)
	licenseIDs := make([]string, 0)
	licenseProducts := make([]string, 0)
	expected := jwt.Expected{
		Time: time.Now(),
	}
	for _, claim := range c.claims {
		sub := claim.Subject
		if isValidEmail(sub) {
			sub = hashSub(sub)
		}
		licenseCustomer = appendIfMissing(licenseCustomer, sub)
		if validateErr := claim.ValidateWithLeeway(expected, licenseLeeway); validateErr != nil {
			c.logger.WithError(validateErr).Warnln("license is not valid")
			continue
		}
		for id, product := range claim.Kopano.Products {
			if id != "" {
				licenseIDs = appendIfMissing(licenseIDs, product.LicenseID)
				licenseProducts = appendIfMissing(licenseProducts, id)
			}
		}
	}

	ch <- ksurveyclient.MustNewConstMap("license_customer", map[string]interface{}{
		"desc":  "Customer IDs of all active or not active licenses",
		"type":  "string[]",
		"value": licenseCustomer,
	})

	ch <- ksurveyclient.MustNewConstMap("license_ids", map[string]interface{}{
		"desc":  "Active license IDs",
		"type":  "string[]",
		"value": licenseIDs,
	})

	ch <- ksurveyclient.MustNewConstMap("license_products", map[string]interface{}{
		"desc":  "Product IDs of all active licenses",
		"type":  "string[]",
		"value": licenseProducts,
	})
}

func appendIfMissing(slice []string, elem string) []string {
	for _, c := range slice {
		if c == elem {
			return slice
		}
	}
	return append(slice, elem)
}
