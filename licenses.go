/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2021 Kopano and its licensors
 */

package kustomer

import (
	"bytes"
	"crypto/x509"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
	"gopkg.in/square/go-jose.v2"
	"gopkg.in/square/go-jose.v2/jwt"

	"stash.kopano.io/kgol/kustomer/license"
)

// DefaultLicenseLeeway is the default leeway when comparing timestamps in licenses.
var DefaultLicenseLeeway = 24 * time.Hour

const (
	licenseSizeLimitBytes = 1024 * 1024
)

// A LicensesLoader defines the parameters how to load licenses from a folder path.
type LicensesLoader struct {
	// If CertPool is set, licenses are validated with it when offline.
	CertPool *x509.CertPool

	// JWKS is the key set for license validation when not offline.
	JWKS *jose.JSONWebKeySet

	// Offline allows license valdation with keys from CertPool if not found in JWKS.
	Offline bool

	// Logger is the logger used. If nil, a standard logger is used.
	Logger logrus.FieldLogger

	// History is a map to avoid loading the same license twice if not nil.
	LoadHistory     map[string]*license.Claims
	ActivateHistory map[string]*license.Claims

	// Hooks.
	OnActivate func(*license.Claims)
	OnRemove   func(*license.Claims)
	OnNew      func(*license.Claims)
	OnSkip     func(*license.Claims)
}

// ScanFolder scans the provided folder for license files, loads, parses and
// validates them all and returns the claim set for each currently valid license.
func (ll *LicensesLoader) ScanFolder(licensesPath string, expected jwt.Expected) ([]*license.Claims, error) {
	return ll.scanFolderForLicenseClaims(licensesPath, expected, false)
}

// UnsafeScanFolderWithoutVerification scans the provided folder for license
// files, loads parses and skips validation if no matching key is found, making
// this function unsafe to use when its required to only return valid license
// claim sets.
func (ll *LicensesLoader) UnsafeScanFolderWithoutVerification(licensesPath string, expected jwt.Expected) ([]*license.Claims, error) {
	return ll.scanFolderForLicenseClaims(licensesPath, expected, true)
}

func (ll *LicensesLoader) scanFolderForLicenseClaims(licensesPath string, expected jwt.Expected, unsafe bool) ([]*license.Claims, error) {
	logger := ll.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	claims := make([]*license.Claims, 0)

	if files, readDirErr := ioutil.ReadDir(licensesPath); readDirErr == nil {
		for _, info := range files {
			if info.IsDir() {
				continue
			}
			fn := filepath.Join(licensesPath, info.Name())
			if f, openErr := os.Open(fn); openErr == nil {
				isNew := true
				c := &license.Claims{
					LicenseID:       fn,
					LicenseFileName: fn,
				}
				if _, ok := ll.LoadHistory[c.LicenseID]; ok {
					isNew = false
				}
				func() {
					r := io.LimitReader(f, licenseSizeLimitBytes)
					if raw, readErr := ioutil.ReadAll(r); readErr == nil {
						c.Raw = bytes.TrimSpace(raw)
						if token, parseErr := jwt.ParseSigned(string(c.Raw)); parseErr == nil {
							if len(token.Headers) != 1 {
								if isNew {
									logger.WithField("name", fn).Warnln("license with multiple headers, ignored")
								}
								return
							}
							headers := token.Headers[0]
							switch jose.SignatureAlgorithm(headers.Algorithm) {
							case jose.EdDSA:
							case jose.ES256:
							case jose.ES384:
							case jose.ES512:
							default:
								if isNew {
									logger.WithFields(logrus.Fields{
										"alg":  headers.Algorithm,
										"name": fn,
									}).Warnln("license with unknown alg, ignored")
								}
								return
							}
							var key interface{}
							if ll.JWKS != nil {
								keys := ll.JWKS.Key(headers.KeyID)
								if len(keys) == 0 && !unsafe {
									if isNew {
										logger.WithFields(logrus.Fields{
											"kid":  headers.KeyID,
											"name": fn,
										}).Warnln("license with unknown kid, ignored")
									}
									return
								} else {
									key = &keys[0]
								}
							}
							if key == nil {
								if !ll.Offline && !unsafe {
									if isNew {
										logger.WithFields(logrus.Fields{
											"kid":  headers.KeyID,
											"name": fn,
										}).Warnln("license found but there is no matching online key, skipped")
									}
									return
								}
								if ll.CertPool != nil {
									// If we have a certificate pool, try to validate the license with it
									// in offline mode.
									chain, certsErr := headers.Certificates(x509.VerifyOptions{
										Roots: ll.CertPool,
									})
									if certsErr != nil {
										if isNew {
											logger.WithError(certsErr).WithFields(logrus.Fields{
												"kid":  headers.KeyID,
												"name": fn,
											}).Warnln("license certificate check failed, skipped")
										}
										return
									}
									if len(chain) > 0 && len(chain[0]) > 0 {
										// Extract public key from chain.
										cert := chain[0][0]
										key = cert.PublicKey
									}
								}
								if key == nil && !unsafe {
									if isNew {
										logger.WithFields(logrus.Fields{
											"kid":  headers.KeyID,
											"name": fn,
										}).Warnln("license found but there is no matching offline key, skipped")
									}
									return
								}
							}
							var claimsErr error
							if unsafe && key == nil {
								claimsErr = token.UnsafeClaimsWithoutVerification(&c)
							} else {
								claimsErr = token.Claims(key, &c)
							}
							if claimsErr == nil {
								if c.Claims.ID != "" {
									c.LicenseID = c.Claims.ID
								}
								if _, ok := ll.LoadHistory[c.LicenseID]; ok {
									isNew = false
								}
								if validateErr := c.Claims.ValidateWithLeeway(expected, DefaultLicenseLeeway); validateErr != nil {
									if isNew {
										logger.WithError(validateErr).WithField("name", fn).Warnln("license is not valid, skipped")
									}
									return
								} else {
									subject := strings.TrimSpace(c.Claims.Subject)
									if subject == "" {
										if isNew {
											logger.WithFields(logrus.Fields{
												"kid":  headers.KeyID,
												"name": fn,
											}).Warnln("license found but it's sub claim is empty, skipped")
										}
										return
									}
								}
							} else {
								if isNew {
									logger.WithError(claimsErr).WithField("name", fn).Errorln("error while parsing license file claims")
								}
								return
							}
						} else {
							if isNew {
								logger.WithError(parseErr).WithField("name", fn).Errorln("error while parsing license file")
							}
							return
						}
					} else {
						logger.WithError(readErr).WithField("name", fn).Errorln("error while reading license file")
						return
					}
					// If reached here, all is good, add claims to result.
					claims = append(claims, c)
					if isNew {
						logger.WithField("name", fn).Debugln("license is valid, loaded")
					}
				}()
				f.Close()
				if isNew && ll.LoadHistory != nil {
					ll.LoadHistory[c.LicenseID] = c
				}
			} else {
				logger.WithError(openErr).WithField("name", fn).Errorln("failed to read license file")
			}
		}
	} else {
		return nil, readDirErr
	}

	return ll.sortAndDeduplicate(claims)
}

func (ll *LicensesLoader) sortAndDeduplicate(claims []*license.Claims) ([]*license.Claims, error) {
	logger := ll.Logger
	if logger == nil {
		logger = logrus.StandardLogger()
	}

	// Sort reverse to prepare for uid deduplication (newer shall win).
	sort.SliceStable(claims, func(i int, j int) bool {
		return claims[i].Claims.IssuedAt.Time().After(claims[j].Claims.IssuedAt.Time())
	})

	// Deduplicate uid, sorted from newer to older, means everything
	// which was seen already can be removed.
	all := make(map[string]*license.Claims)
	added := make(map[string]bool)
	claims = func(claims []*license.Claims) []*license.Claims {
		result := make([]*license.Claims, 0)
		seen := make(map[string]bool)
		for _, c := range claims {
			all[c.LicenseID] = c
			isNew := true
			if _, ok := ll.ActivateHistory[c.LicenseID]; ok {
				added[c.LicenseID] = false
				isNew = false
			} else {
				added[c.LicenseID] = true
				if ll.ActivateHistory != nil {
					ll.ActivateHistory[c.LicenseID] = c
				}
			}
			if !seen[c.LicenseFileID] {
				if c.LicenseFileID != "" {
					seen[c.LicenseFileID] = true
				}
				// Prepend to also reverse.
				result = append([]*license.Claims{c}, result...)
				if isNew && ll.OnActivate != nil {
					ll.OnActivate(c)
				}
			} else {
				if isNew && ll.OnSkip != nil {
					ll.OnSkip(c)
				}
			}
		}
		return result
	}(claims)

	// Trigger hooks, based on what is removed or now.
	for k, c := range ll.ActivateHistory {
		if _, ok := added[k]; !ok {
			delete(ll.ActivateHistory, k)
			if ll.OnRemove != nil {
				ll.OnRemove(c)
			}
		}
	}
	for k, v := range added {
		if v {
			c := all[k]
			if ll.OnNew != nil {
				ll.OnNew(c)
			}
		}
	}

	return claims, nil
}
