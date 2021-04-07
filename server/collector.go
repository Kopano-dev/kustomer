/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/square/go-jose.v2/jwt"
	"stash.kopano.io/kgol/ksurveyclient-go"

	"stash.kopano.io/kgol/kustomer"
	"stash.kopano.io/kgol/kustomer/license"
)

type Collector struct {
	mutex sync.RWMutex

	claims []*license.Claims
	logger logrus.FieldLogger
}

func NewCollector(c *Config, claims []*license.Claims) (*Collector, error) {
	return &Collector{
		claims: claims,
		logger: c.Logger,
	}, nil
}

func (c *Collector) setClaims(claims []*license.Claims) {
	c.mutex.Lock()
	c.claims = claims
	c.mutex.Unlock()
}

func (c *Collector) Collect(ch chan<- ksurveyclient.Metric) {
	licenseCustomer := make([]string, 0)
	licenseIDs := make([]string, 0)
	licenseProducts := make([]string, 0)
	expected := jwt.Expected{
		Time: time.Now(),
	}

	c.mutex.RLock()
	claims := c.claims
	c.mutex.RUnlock()
	for _, claim := range claims {
		sub := claim.Subject
		if isValidEmail(sub) {
			sub = hashSub(sub)
		}
		licenseCustomer = appendIfMissing(licenseCustomer, sub)
		if validateErr := claim.ValidateWithLeeway(expected, kustomer.DefaultLicenseLeeway); validateErr != nil {
			c.logger.WithField("name", claim.LicenseFileName).WithError(validateErr).Warnln("license is not valid")
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

	c.logger.WithField("customer", licenseCustomer).Debugln("collecting license ids")
	c.logger.WithField("ids", licenseIDs).Debugln("collecting active license ids")
	c.logger.WithField("products", licenseProducts).Debugln("collecting active licensed product ids")
}

func appendIfMissing(slice []string, elem string) []string {
	for _, c := range slice {
		if c == elem {
			return slice
		}
	}
	return append(slice, elem)
}
