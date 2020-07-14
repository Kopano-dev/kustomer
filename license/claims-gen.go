/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2020 Kopano and its licensors
 */

package license

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

// GenerateCaims is a helper to generate Kopano product license claims from
// a map of string key/values.
func GenerateClaims(params map[string][]string) (*Claims, error) {
	uid, err := uuid.NewRandom()
	if err != nil {
		return nil, fmt.Errorf("failed to generate uid: %w", err)
	}

	claims := &Claims{
		LicenseFileID: uid.String(),
	}
	claims.Kopano = Kopano{
		Version:  0,
		Products: make(ProductSet),
	}

	for k, values := range params {
		if len(values) > 1 {
			return nil, fmt.Errorf("multiple values for key %v", k)
		}
		v := values[0]
		switch k {
		case "uid":
			claims.LicenseFileID = v
		default:
			parts := strings.SplitN(k, ".", 2)
			if len(parts) != 2 {
				return nil, fmt.Errorf("unsupported format in key %v", k)
			}

			product, ok := claims.Kopano.Products[parts[0]]
			if !ok {
				lid, lidErr := uuid.NewRandom()
				if lidErr != nil {
					return nil, fmt.Errorf("failed to generate lid: %w", lidErr)
				}
				product = &Product{
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
						return nil, fmt.Errorf("failed to parse int value for %v: %w", k, parseErr)
					}
					tv = iv
				case "bool":
					bv, parseErr := strconv.ParseBool(v)
					if parseErr != nil {
						return nil, fmt.Errorf("failed to parse bool value for %v: %w", k, parseErr)
					}
					tv = bv
				case "float":
					fv, parseErr := strconv.ParseFloat(v, 64)
					if parseErr != nil {
						return nil, fmt.Errorf("failed to parse float value for %v: %w", k, parseErr)
					}
					tv = fv
				}
				product.Unknown[k] = tv
			}
		}
	}

	return claims, nil
}
