/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package server

import (
	"context"
	"net"

	"golang.org/x/sys/unix"
)

type contextKey struct {
	id string
}

// UcredContextKey is a context key. It can be used in HTTP handlers with
// Context.Value to access the Ucred struct of the request connection. The
// associated value will be of type *syscall.Ucred
var UcredContextKey = &contextKey{"http-server"}

// handleConnectionContext is a http.Server ConnContext hook, which injects
// unix socket credentials into the request context.
func (s *Server) handleConnectionContext(ctx context.Context, c net.Conn) context.Context {
	conn, _ := c.(*net.UnixConn)
	if conn == nil {
		return ctx
	}
	f, _ := conn.File()
	if f == nil {
		return ctx
	}
	defer f.Close()
	ucred, _ := unix.GetsockoptUcred(int(f.Fd()), unix.SOL_SOCKET, unix.SO_PEERCRED)
	return context.WithValue(ctx, UcredContextKey, ucred)
}

// GetUcredContextValue returns the ucred value from the provided context if
// there is any.
func GetUcredContextValue(ctx context.Context) (*unix.Ucred, bool) {
	v := ctx.Value(UcredContextKey)
	if v == nil {
		return nil, false
	}

	ucred, ok := v.(*unix.Ucred)
	return ucred, ok
}
