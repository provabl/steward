// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/provabl/steward/internal/store"
)

func TestProvenanceRecord_WritesRecord(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".steward")
	nowFunc = func() time.Time { return time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = time.Now })

	cmd := provenanceCmd()
	cmd.SetArgs([]string{
		"record",
		"--dataset", "phs000178",
		"--dest", "s3://sre/genomic/phs000178/",
		"--source", "globus:dtn/dbgap/phs000178",
		"--digest", "sha256:abc",
		"--dua-id", "DUA-2025-001",
		"--data-class", "GENOMIC",
		"--authorized-by", "compliance@mru.edu",
		"--steward-dir", dir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("record: %v", err)
	}

	rec, err := store.New(dir).LoadRecord("s3://sre/genomic/phs000178/")
	if err != nil || rec == nil {
		t.Fatalf("expected a saved record, got (%v, %v)", rec, err)
	}
	if rec.Dataset != "phs000178" || rec.DUAID != "DUA-2025-001" || rec.DataClass != "GENOMIC" {
		t.Errorf("record fields wrong: %+v", rec)
	}
	// record always writes integrity_verified=false; only verify (PR5) flips it.
	if rec.IntegrityVerified {
		t.Error("record must set integrity_verified=false (verify confirms it later)")
	}
	if !rec.RecordedAt.Equal(time.Date(2026, 6, 24, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("RecordedAt = %v, want the injected time", rec.RecordedAt)
	}
}

func TestProvenanceRecord_RequiresDatasetAndDest(t *testing.T) {
	// Missing --dest (cobra enforces required flags → Execute errors).
	cmd := provenanceCmd()
	cmd.SetArgs([]string{"record", "--dataset", "phs000178", "--steward-dir", t.TempDir()})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil {
		t.Error("record without --dest: want error, got nil")
	}
}
