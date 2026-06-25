// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/provabl/steward/internal/audit"
	"github.com/provabl/steward/internal/store"
)

func logCmd() *cobra.Command {
	var (
		dataClass  string
		duaID      string
		asJSON     bool
		stewardDir string
	)
	cmd := &cobra.Command{
		Use:   "log",
		Short: "Show the data-ingestion audit trail",
		Long: `List every recorded ingestion — dataset, source, governing DUA, data class, and
whether steward has recomputed-and-matched its digest (integrity_verified) —
newest first. This is the record an auditor asks for: what data came into the
account, from where, and under what authority. Filter by data class or DUA.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			recs, err := audit.Query(store.New(stewardDir), audit.Filter{DataClass: dataClass, DUAID: duaID})
			if err != nil {
				return err
			}
			if asJSON {
				enc := json.NewEncoder(os.Stdout)
				enc.SetIndent("", "  ")
				return enc.Encode(recs)
			}
			if len(recs) == 0 {
				fmt.Println("No ingestion records.")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
			fmt.Fprintln(w, "RECORDED\tDATASET\tCLASS\tDUA\tVERIFIED\tSOURCE")
			for _, r := range recs {
				verified := "no"
				if r.IntegrityVerified {
					verified = "yes"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					r.RecordedAt.Format("2006-01-02"), dash(r.Dataset), dash(r.DataClass),
					dash(r.DUAID), verified, dash(r.Source))
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&dataClass, "data-class", "", "filter to a single data class, e.g. GENOMIC")
	cmd.Flags().StringVar(&duaID, "dua-id", "", "filter to a single governing DUA")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the records as JSON")
	cmd.Flags().StringVar(&stewardDir, "steward-dir", ".steward", "store directory")
	return cmd
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
