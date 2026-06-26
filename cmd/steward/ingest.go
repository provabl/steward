// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/provabl/steward/internal/ingest"
	"github.com/provabl/steward/internal/mover"
	"github.com/provabl/steward/internal/store"
)

func ingestCmd() *cobra.Command {
	var (
		dataset, source, dest        string
		duaID, dataClass, principal  string
		allowedDUAs, allowedSources  []string
		requireDataClass, stewardDir string
	)
	cmd := &cobra.Command{
		Use:   "ingest",
		Short: "Authorize, move, and record a move-to-compute data ingestion",
		Long: `Bring data INTO a secure account under governance: verify the ingestion is
authorized *before* any bytes move, drive the configured mover, then record the
provenance the rest of steward governs (gate, log, verify).

The flow is authorize → move → record, fail-closed at every step: an
unauthorized request never moves bytes, a failed move never writes a record, and
the recorded digest is the mover's *asserted* value (integrity_verified=false)
until 'steward provenance verify' recomputes it against the destination.

v1 authorization is config-driven (--allowed-dua / --allowed-source /
--require-data-class) and the only mover is the local reference mover (local
paths / file://). The IAM-tag authorizer (reading attest:nih-dua-ids) and the
live movers (Globus / DataSync / s3cp) are deferred — same seam, no code change
to this command.

Example:
  steward ingest --dataset phs000178 \
    --source ./incoming/phs000178.tar --dest ./sre/genomic/phs000178.tar \
    --dua-id DUA-2025-001 --data-class GENOMIC \
    --principal arn:aws:iam::123456789012:role/AliceResearchRole \
    --allowed-dua DUA-2025-001 --allowed-source ./incoming/`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dataset == "" || source == "" || dest == "" {
				return fmt.Errorf("--dataset, --source, and --dest are required")
			}
			auth := ingest.PolicyAuthorizer{
				AllowedDUAs:      allowedDUAs,
				AllowedSources:   allowedSources,
				RequireDataClass: requireDataClass,
			}
			ing := ingest.New(auth, mover.NewLocalMover(), store.New(stewardDir))
			res, err := ing.Ingest(cmd.Context(), ingest.Request{
				Dataset:   dataset,
				Source:    source,
				Dest:      dest,
				DUAID:     duaID,
				DataClass: dataClass,
				Principal: principal,
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Ingested %s (%d bytes via %s)\n", res.Record.Dataset, res.BytesMoved, res.Record.Mover)
			fmt.Printf("  dest:   %s\n", res.Record.Dest)
			fmt.Printf("  digest: %s (asserted)\n", res.Record.Digest)
			fmt.Println("  integrity_verified: false — run 'steward provenance verify' to confirm, then 'steward gate'")
			return nil
		},
	}
	cmd.Flags().StringVar(&dataset, "dataset", "", "dataset / study id, e.g. phs000178 (required)")
	cmd.Flags().StringVar(&source, "source", "", "mover-scheme source, e.g. ./incoming/phs000178.tar (required)")
	cmd.Flags().StringVar(&dest, "dest", "", "destination the data lands at (required)")
	cmd.Flags().StringVar(&duaID, "dua-id", "", "governing Data Use Agreement id")
	cmd.Flags().StringVar(&dataClass, "data-class", "", "data class, e.g. GENOMIC")
	cmd.Flags().StringVar(&principal, "principal", "", "requesting principal (ARN) recorded as authorized_by")
	cmd.Flags().StringSliceVar(&allowedDUAs, "allowed-dua", nil, "a DUA id the principal is approved for (repeatable)")
	cmd.Flags().StringSliceVar(&allowedSources, "allowed-source", nil, "an allowed source prefix (repeatable)")
	cmd.Flags().StringVar(&requireDataClass, "require-data-class", "", "require the request's data class to match this")
	cmd.Flags().StringVar(&stewardDir, "steward-dir", ".steward", "store directory")
	return cmd
}
