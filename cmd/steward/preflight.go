// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/provabl/steward/internal/preflight"
)

func preflightCmd() *cobra.Command {
	var region string
	cmd := &cobra.Command{
		Use:   "preflight",
		Short: "Verify the calling principal holds the IAM permissions steward needs",
		Long: `Check that the calling AWS principal can perform steward's AWS-touching actions
(s3:GetObject for verifying an S3 destination's digest; s3:PutObjectTagging /
s3:PutObjectRetention for the deferred apply-handling path), via read-only
iam:SimulatePrincipalPolicy against the caller — it evaluates, it does not act.
A denied action prints a remediation and the command exits non-zero. See
docs/required-permissions.md.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPreflight(preflight.CheckCallerPermissions(cmd.Context(), region))
		},
	}
	cmd.Flags().StringVar(&region, "region", "us-east-1", "AWS region")
	return cmd
}

// runPreflight renders preflight results and returns a non-nil error if any failed.
func runPreflight(results []preflight.Result) error {
	failures := 0
	for _, r := range results {
		if r.Status {
			fmt.Printf("  ✓ %s\n", r.Name)
			continue
		}
		failures++
		fmt.Printf("  ✗ %s: %s\n", r.Name, r.Detail)
		if r.Remediation != "" {
			fmt.Printf("      Remediation: %s\n", r.Remediation)
		}
	}
	fmt.Println()
	if failures > 0 {
		return fmt.Errorf("preflight failed: %d required permission(s) missing", failures)
	}
	fmt.Println("✓ All required permissions present")
	return nil
}
