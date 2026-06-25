// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Command steward governs move-to-compute data ingestion for the Provabl suite:
// it records the provenance of data brought into a secure account, appraises it
// through the provabl/evidence kernel's data:// provider, and writes the
// .steward/gate-result.json file attest's Cedar PDP reads as context.data.*.
//
// Where vet qualifies the software that arrives at an SRE, steward qualifies the
// data. See the suite spec (business/steward-product-spec.md) and ADR 0004.
//
// v1 scope: provenance record/verify, the data:// appraisal gate, audit log, and
// preflight. Transport (the mover), S3 Object Lock handling, and closeout/
// destruction are deferred — v1 governs data that was moved out-of-band.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

func main() {
	if err := rootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func rootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "steward",
		Short: "Data-ingestion stewardship for the Provabl suite",
		Long: `steward governs move-to-compute data ingestion: it records the provenance of
data brought into a secure account, appraises it through the provabl/evidence
kernel, and writes the .steward/gate-result.json attest's Cedar PDP reads as
context.data.*. Where vet qualifies the software, steward qualifies the data.`,
		Version: version,
		// main() prints the error and sets the exit code; don't double-print or
		// dump usage on a runtime (RunE) failure.
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.AddCommand(provenanceCmd(), gateCmd(), logCmd())
	// Further subcommands are added in their own PRs: preflight.
	return cmd
}
