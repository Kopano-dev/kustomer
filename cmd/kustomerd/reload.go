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

func commandReload() *cobra.Command {
	reloadCmd := &cobra.Command{
		Use:   "reload",
		Short: "Perform reload",
		Run: func(cmd *cobra.Command, args []string) {
			if err := reload(cmd, args); err != nil {
				fmt.Fprintf(os.Stderr, "Error: %v\n", err)
				os.Exit(1)
			}
		},
	}

	reloadCmd.Flags().StringVar(&listenPath, "listen-path", listenPath, "Path to unix socket for API requests")

	return reloadCmd
}

func reload(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	uri := url.URL{
		Scheme: "http",
		Host:   "localhost",
		Path:   "/reload",
	}
	client := http.Client{
		Timeout: time.Second * 60,
		Transport: &http.Transport{
			Dial: func(proto, addr string) (conn net.Conn, err error) {
				return net.Dial("unix", listenPath)
			},
		},
	}

	request, err := http.NewRequest(http.MethodPost, uri.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to create reload request: %v", err)
	}

	request.Header.Set("Connection", "close")
	request.Header.Set("User-Agent", server.DefaultHTTPUserAgent)
	request = request.WithContext(ctx)

	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("reload request failed: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(response.Body)
		fmt.Fprint(os.Stderr, string(bodyBytes))

		return fmt.Errorf("reload failed with status: %v", response.StatusCode)
	} else {
		fmt.Fprint(os.Stdout, "reload successful\n")
	}

	return nil
}
