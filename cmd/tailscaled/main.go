// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

// The tailscale command is the Tailscale command-line client. It interacts
// with the tailscaled node agent.
package main // import "tailscale.com/cmd/tailscale"

import (
	"os"

	"tailscale.com/cmd/tailscaled/cli"
)

func main() {
	args := os.Args[1:]
	cli.Run(args)
}
