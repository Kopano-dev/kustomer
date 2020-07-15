/*
 * SPDX-License-Identifier: AGPL-3.0-or-later
 * Copyright 2019 Kopano and its licensors
 */

package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"

	"stash.kopano.io/kgol/kustomer/server"
)

func commandHealthcheck() *cobra.Command {
	healthcheckCmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Perform health check",
		Run: func(cmd *cobra.Command, args []string) {
			if err := healthcheck(cmd, args); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	healthcheckCmd.Flags().StringVar(&listenPath, "listen-path", listenPath, "Path to unix socket for API requests")
	healthcheckCmd.Flags().String("path", "/health-check", "URL path and optional parameters to health-check endpoint")

	return healthcheckCmd
}

func healthcheck(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	uri := url.URL{
		Scheme: "http",
		Host:   "localhost",
	}
	uri.Path, _ = cmd.Flags().GetString("path")

	var dialer net.Dialer
	client := http.Client{
		Timeout: time.Second * 60,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, proto, addr string) (conn net.Conn, err error) {
				return dialer.DialContext(ctx, "unix", listenPath)
			},
			DisableKeepAlives: true,
		},
	}

	request, err := http.NewRequest(http.MethodPost, uri.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create healthcheck request: %v", err)
	}

	request.Header.Set("Connection", "close")
	request.Header.Set("User-Agent", server.DefaultHTTPUserAgent)
	request = request.WithContext(ctx)

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("healthcheck request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(response.Body)
		fmt.Fprint(os.Stderr, string(bodyBytes))

		return fmt.Errorf("healthcheck failed with status: %v", response.StatusCode)
	} else {
		fmt.Fprint(os.Stdout, "healthcheck successful\n")
	}

	return nil
}
