// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/provabl/steward/internal/store"
)

// nowFunc is overridable in tests so RecordedAt is deterministic.
var nowFunc = time.Now

func provenanceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provenance",
		Short: "Record and verify data-ingestion provenance",
		Long: `Record the provenance of data brought into a secure account (move-to-compute),
and (later) verify the recorded digest against the bytes at the destination.`,
	}
	cmd.AddCommand(provenanceRecordCmd())
	return cmd
}

func provenanceRecordCmd() *cobra.Command {
	var (
		dataset      string
		dest         string
		source       string
		digest       string
		duaID        string
		dataClass    string
		authorizedBy string
		mover        string
		stewardDir   string
	)
	cmd := &cobra.Command{
		Use:   "record",
		Short: "Record the provenance of an ingested dataset",
		Long: `Record where an ingested dataset came from, its content digest, the governing
DUA, and the authorizing principal, as a durable provenance record under
.steward/records/. This records an *asserted* digest; it is marked
integrity_verified=false until 'steward provenance verify' recomputes it against
the destination — so the provenance claim means "steward recomputed and matched,"
not "someone told us."`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dataset == "" || dest == "" {
				return fmt.Errorf("--dataset and --dest are required")
			}
			rec := &store.ProvenanceRecord{
				Dataset:           dataset,
				Dest:              dest,
				Source:            source,
				Digest:            digest,
				IntegrityVerified: false, // set true only by 'provenance verify' (PR5)
				DUAID:             duaID,
				DataClass:         dataClass,
				AuthorizedBy:      authorizedBy,
				Mover:             mover,
				RecordedAt:        nowFunc().UTC(),
			}
			if err := store.New(stewardDir).SaveRecord(rec); err != nil {
				return err
			}
			fmt.Printf("✓ Recorded provenance for %s\n", dataset)
			fmt.Printf("  dest:   %s\n", dest)
			if source != "" {
				fmt.Printf("  source: %s\n", source)
			}
			if duaID != "" {
				fmt.Printf("  DUA:    %s\n", duaID)
			}
			fmt.Println("  integrity_verified: false — run 'steward provenance verify' to confirm the digest")
			return nil
		},
	}
	cmd.Flags().StringVar(&dataset, "dataset", "", "dataset / study id, e.g. phs000178 (required)")
	cmd.Flags().StringVar(&dest, "dest", "", "destination the data landed at, e.g. s3://bucket/genomic/phs000178/ (required)")
	cmd.Flags().StringVar(&source, "source", "", "where the data came from, e.g. globus:dtn.ncbi.nlm.nih.gov/dbgap/phs000178")
	cmd.Flags().StringVar(&digest, "digest", "", "asserted content digest (sha256:...) of the ingested data")
	cmd.Flags().StringVar(&duaID, "dua-id", "", "governing Data Use Agreement id")
	cmd.Flags().StringVar(&dataClass, "data-class", "", "data class, e.g. GENOMIC")
	cmd.Flags().StringVar(&authorizedBy, "authorized-by", "", "who authorized the ingestion")
	cmd.Flags().StringVar(&mover, "mover", "out-of-band", "transport used: globus | datasync | s3cp | out-of-band")
	cmd.Flags().StringVar(&stewardDir, "steward-dir", ".steward", "store directory")
	_ = cmd.MarkFlagRequired("dataset")
	_ = cmd.MarkFlagRequired("dest")
	return cmd
}
