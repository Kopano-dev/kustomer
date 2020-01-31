/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"github.com/sirupsen/logrus"
)

// Config bundles configuration settings.
type Config struct {
	Sub string

	Logger logrus.FieldLogger
}
