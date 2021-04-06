/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"crypto/x509"
	"net/url"

	"github.com/sirupsen/logrus"
)

// Config bundles configuration settings.
type Config struct {
	Sub string

	LicensesPath string
	ListenPath   string

	Insecure bool

	Trusted  bool
	JWKSURIs []*url.URL
	CertPool *x509.CertPool

	Logger logrus.FieldLogger

	OnFirstClaims func(*Server)
}
