// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package store

import (
	"testing"
	"time"
)

func newRecord(dataset, dest string) *ProvenanceRecord {
	return &ProvenanceRecord{
		Dataset:           dataset,
		Dest:              dest,
		Source:            "globus:dtn.ncbi.nlm.nih.gov/dbgap/" + dataset,
		Digest:            "sha256:abc123",
		IntegrityVerified: true,
		DUAID:             "DUA-2025-001",
		DataClass:         "GENOMIC",
		AuthorizedBy:      "compliance@mru.edu",
		Mover:             "out-of-band",
		RecordedAt:        time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	s := New(t.TempDir())
	rec := newRecord("phs000178", "s3://sre/genomic/phs000178/")
	if err := s.SaveRecord(rec); err != nil {
		t.Fatalf("SaveRecord: %v", err)
	}
	got, err := s.LoadRecord(rec.Dest)
	if err != nil {
		t.Fatalf("LoadRecord: %v", err)
	}
	if got == nil {
		t.Fatal("LoadRecord returned nil for a saved record")
	}
	if got.Dataset != "phs000178" || got.DUAID != "DUA-2025-001" || !got.IntegrityVerified {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if !got.RecordedAt.Equal(rec.RecordedAt) {
		t.Errorf("RecordedAt = %v, want %v", got.RecordedAt, rec.RecordedAt)
	}
}

func TestLoadMissingIsNilNil(t *testing.T) {
	s := New(t.TempDir())
	got, err := s.LoadRecord("s3://nope/")
	if err != nil {
		t.Fatalf("LoadRecord on missing: want nil error, got %v", err)
	}
	if got != nil {
		t.Errorf("LoadRecord on missing: want nil record, got %+v", got)
	}
}

func TestSaveOverwritesByDest(t *testing.T) {
	s := New(t.TempDir())
	dest := "s3://sre/genomic/phs000178/"
	if err := s.SaveRecord(newRecord("phs000178", dest)); err != nil {
		t.Fatal(err)
	}
	// Re-record the same dest with a different DUA — should supersede, not duplicate.
	updated := newRecord("phs000178", dest)
	updated.DUAID = "DUA-2027-099"
	if err := s.SaveRecord(updated); err != nil {
		t.Fatal(err)
	}
	got, _ := s.LoadRecord(dest)
	if got.DUAID != "DUA-2027-099" {
		t.Errorf("DUAID = %q, want the superseding DUA-2027-099", got.DUAID)
	}
	recs, _ := s.ListRecords()
	if len(recs) != 1 {
		t.Errorf("same dest should yield 1 record, got %d", len(recs))
	}
}

func TestListRecords(t *testing.T) {
	s := New(t.TempDir())
	if recs, err := s.ListRecords(); err != nil || recs != nil {
		t.Errorf("empty store: want (nil,nil), got (%v,%v)", recs, err)
	}
	_ = s.SaveRecord(newRecord("phs000178", "s3://sre/a/"))
	_ = s.SaveRecord(newRecord("phs000200", "s3://sre/b/"))
	recs, err := s.ListRecords()
	if err != nil {
		t.Fatalf("ListRecords: %v", err)
	}
	if len(recs) != 2 {
		t.Fatalf("got %d records, want 2", len(recs))
	}
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.Dataset] = true
	}
	if !seen["phs000178"] || !seen["phs000200"] {
		t.Errorf("missing datasets: %v", seen)
	}
}

func TestSaveGateResult(t *testing.T) {
	s := New(t.TempDir())
	g := &GateResult{Dataset: "phs000178", Dest: "s3://sre/x/", ProvenanceVerified: true, PolicyMet: true, EvaluatedAt: time.Now()}
	if err := s.SaveGateResult(g); err != nil {
		t.Fatalf("SaveGateResult: %v", err)
	}
}
