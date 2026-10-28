// Copyright (c) Tailscale Inc & contributors
// SPDX-License-Identifier: BSD-3-Clause

package iosdeps

import (
	"testing"

	"tailscale.com/tstest/deptest"
)

func TestDeps(t *testing.T) {
	// This fork includes the control WebSocket transport on iOS, so wsconn
	// and coder/websocket are expected despite upstream issue 13762.
	deptest.DepChecker{
		GOOS:   "ios",
		GOARCH: "arm64",
		BadDeps: map[string]string{
			"testing":                             "do not use testing package in production code",
			"text/template":                       "linker bloat (MethodByName)",
			"html/template":                       "linker bloat (MethodByName)",
			"github.com/mitchellh/go-ps":          "https://github.com/tailscale/tailscale/pull/13759",
			"database/sql/driver":                 "iOS doesn't use an SQL database",
			"github.com/google/uuid":              "see tailscale/tailscale#13760",
			"tailscale.com/clientupdate/distsign": "downloads via AppStore, not distsign",
			"github.com/tailscale/hujson":         "no config file support on iOS",
			"tailscale.com/feature/capture":       "no debug packet capture on iOS",
		},
	}.Check(t)
}
