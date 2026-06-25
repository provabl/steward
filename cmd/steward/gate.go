// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/provabl/steward/internal/gate"
	"github.com/provabl/steward/internal/store"
)

func gateCmd() *cobra.Command {
	var (
		dest             string
		duaRequired      bool
		requireDataClass string
		allowedSources   string
		stewardDir       string
	)
	cmd := &cobra.Command{
		Use:   "gate",
		Short: "Evaluate a destination's ingestion provenance and write .steward/gate-result.json",
		Long: `Evaluate the provenance recorded for an ingested destination against a policy,
through the provabl/evidence kernel, and write the context.data.* attributes
attest's Cedar PDP reads to .steward/gate-result.json.

Fail-closed: a destination with no provenance record, or one whose digest has not
been verified ('steward provenance verify'), does not pass.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dest == "" {
				return fmt.Errorf("--dest is required")
			}
			policy := &gate.Policy{
				DUARequired:      duaRequired,
				RequireDataClass: requireDataClass,
				AllowedSources:   allowedSources,
			}
			res, err := gate.New(store.New(stewardDir), policy).Evaluate(cmd.Context(), dest)
			if err != nil {
				return err
			}
			g := res.GateResult
			fmt.Printf("Data-ingestion gate: %s\n\n", dest)
			fmt.Printf("  context.data.ProvenanceVerified = %v\n", g.ProvenanceVerified)
			fmt.Printf("  context.data.SourceVerified     = %v\n", g.SourceVerified)
			fmt.Printf("  context.data.IntegrityChecked   = %v\n", g.IntegrityChecked)
			fmt.Printf("  context.data.DUAId              = %s\n", g.DUAID)
			fmt.Printf("  context.data.DataClass          = %s\n", g.DataClass)
			fmt.Printf("\n✓ Written to %s/gate-result.json\n", stewardDir)
			if !res.PolicyMet {
				fmt.Println("\n✗ Policy not met:")
				for _, f := range res.Failures {
					fmt.Printf("  - %s\n", f)
				}
				os.Exit(1)
			}
			fmt.Println("\n✓ Policy met")
			return nil
		},
	}
	cmd.Flags().StringVar(&dest, "dest", "", "ingested destination to gate, e.g. s3://bucket/genomic/phs000178/ (required)")
	cmd.Flags().BoolVar(&duaRequired, "dua-required", true, "fail if the record has no governing DUA")
	cmd.Flags().StringVar(&requireDataClass, "require-data-class", "", "require the record's data class to match (e.g. GENOMIC)")
	cmd.Flags().StringVar(&allowedSources, "allowed-sources", "", "'+'-delimited allowed source-URI prefixes")
	cmd.Flags().StringVar(&stewardDir, "steward-dir", ".steward", "store directory")
	_ = cmd.MarkFlagRequired("dest")
	return cmd
}
