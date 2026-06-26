// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/provabl/steward/internal/handling"
)

func applyHandlingCmd() *cobra.Command {
	var (
		dest, dataClass, duaID, retainUntil, region string
	)
	cmd := &cobra.Command{
		Use:   "apply-handling",
		Short: "Tag an ingested destination with its data class and apply Object Lock retention",
		Long: `Apply the storage controls a data class requires to an ingested S3 destination:
the data-class object tag and, with --retain-until, S3 Object Lock (COMPLIANCE
mode) retention aligned to the DUA term. This is what makes "data was brought in
AND handled correctly" provable rather than hoped (spec §3).

Handling may be STRENGTHENED but never RELAXED: extending an existing retention
is allowed, shortening or removing one is refused — a DUA term cannot be
weakened. The check reads the current handling first and fails closed if it
cannot.

Needs live AWS: s3:ListBucket/GetObjectTagging/PutObjectTagging and (with
--retain-until) s3:GetObjectRetention/PutObjectRetention, plus a bucket with
Object Lock enabled. Run 'steward preflight' to confirm the principal holds them.

Example:
  steward apply-handling s3://chen-genomics-sre/genomic/phs000178/ \
    --data-class GENOMIC --dua-id DUA-2025-001 --retain-until 2027-05-01`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dest = args[0]
			if dataClass == "" {
				return fmt.Errorf("--data-class is required")
			}
			var until *time.Time
			if retainUntil != "" {
				t, err := time.Parse("2006-01-02", retainUntil)
				if err != nil {
					return fmt.Errorf("--retain-until must be YYYY-MM-DD: %w", err)
				}
				until = &t
			}
			backend, err := handling.NewS3Backend(cmd.Context(), region)
			if err != nil {
				return err
			}
			res, err := handling.New(backend).Apply(cmd.Context(), handling.Spec{
				Dest: dest, DataClass: dataClass, DUAID: duaID, RetainUntil: until,
			})
			if err != nil {
				return err
			}
			fmt.Printf("✓ Applied handling to %s (via %s)\n", res.Dest, res.Backend)
			fmt.Printf("  data-class: %s\n", res.DataClass)
			if res.RetentionSet {
				fmt.Printf("  retention:  Object Lock until %s (COMPLIANCE)\n", res.RetainUntil.Format("2006-01-02"))
			} else {
				fmt.Println("  retention:  none (no --retain-until)")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dataClass, "data-class", "", "data class to tag the destination with, e.g. GENOMIC (required)")
	cmd.Flags().StringVar(&duaID, "dua-id", "", "governing DUA id (recorded alongside the retention)")
	cmd.Flags().StringVar(&retainUntil, "retain-until", "", "Object Lock retain-until date (YYYY-MM-DD), aligned to the DUA term")
	cmd.Flags().StringVar(&region, "region", "us-east-1", "AWS region")
	return cmd
}
