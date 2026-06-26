// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/provabl/steward/internal/closeout"
	"github.com/provabl/steward/internal/store"
)

func closeoutCmd() *cobra.Command {
	var (
		duaID, principal, region, stewardDir string
		confirm                              bool
	)
	cmd := &cobra.Command{
		Use:   "closeout <s3-dest>",
		Short: "Destroy a dataset at DUA closeout and emit a destruction certificate",
		Long: `Destroy controlled data at the end of its DUA term and record the destruction
certificate an auditor asks for — the lifecycle bookend of ingestion (spec §5).

This is steward's highest-consequence operation: it DELETES controlled data. It
is "certify + confirm, never silent delete":

  • By default it is a DRY RUN — it checks the gates and reports what WOULD be
    destroyed, deleting nothing.
  • --confirm (with --principal) is required to actually destroy.

Before destroying anything it enforces, fail-closed:
  1. a provenance record must exist for the destination (steward only closes out
     data it governs),
  2. any S3 Object Lock retention must have fully ELAPSED (destroying mid-term
     would itself violate the DUA), and
  3. the destruction must be explicitly confirmed by a named principal.

Examples:
  steward closeout s3://sre/genomic/phs000178/ --dua-id DUA-2025-001          # dry run
  steward closeout s3://sre/genomic/phs000178/ --dua-id DUA-2025-001 \
      --confirm --principal arn:aws:iam::…:role/Compliance                    # destroy + certify`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest := args[0]
			destroyer, err := closeout.NewS3Destroyer(cmd.Context(), region)
			if err != nil {
				return err
			}
			out, err := closeout.New(destroyer, store.New(stewardDir)).Closeout(cmd.Context(), dest, closeout.Request{
				DUAID: duaID, Confirm: confirm, Principal: principal,
			})
			if err != nil {
				return err
			}
			if out.DryRun {
				fmt.Printf("DRY RUN — nothing destroyed.\n")
				fmt.Printf("  %d object(s) under %s would be destroyed.\n", out.Objects, out.Dest)
				fmt.Printf("  Retention: %s.\n", retentionNote(out.RetentionCleared))
				fmt.Printf("  Re-run with --confirm --principal <arn> to destroy.\n")
				return nil
			}
			c := out.Certificate
			fmt.Printf("✓ Destroyed %s (%d object versions) at DUA closeout\n", c.Dataset, c.ObjectsDestroyed)
			fmt.Printf("  dua:        %s\n", c.DUAID)
			fmt.Printf("  by:         %s\n", c.DestroyedBy)
			fmt.Printf("  at:         %s\n", c.DestroyedAt.Format("2006-01-02T15:04:05Z"))
			fmt.Printf("  certificate: %s/certificates/\n", stewardDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&duaID, "dua-id", "", "governing DUA id (must match the provenance record when set)")
	cmd.Flags().BoolVar(&confirm, "confirm", false, "actually destroy (default is a dry run)")
	cmd.Flags().StringVar(&principal, "principal", "", "the principal authorizing the destruction (required with --confirm)")
	cmd.Flags().StringVar(&region, "region", "us-east-1", "AWS region")
	cmd.Flags().StringVar(&stewardDir, "steward-dir", ".steward", "store directory")
	return cmd
}

func retentionNote(cleared bool) string {
	if cleared {
		return "elapsed (or none) — destruction permitted"
	}
	return "still active — destruction would be refused"
}
