// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package audit

import (
	"testing"
	"time"

	"github.com/provabl/steward/internal/store"
)

func seedStore(t *testing.T) *store.Store {
	t.Helper()
	s := store.New(t.TempDir())
	recs := []store.ProvenanceRecord{
		{Dataset: "phs000178", Dest: "s3://a/", DUAID: "DUA-1", DataClass: "GENOMIC", RecordedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		{Dataset: "phs000200", Dest: "s3://b/", DUAID: "DUA-2", DataClass: "GENOMIC", RecordedAt: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)},
		{Dataset: "patients", Dest: "s3://c/", DUAID: "DUA-3", DataClass: "PHI", RecordedAt: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)},
	}
	for i := range recs {
		if err := s.SaveRecord(&recs[i]); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

func TestQuery_AllNewestFirst(t *testing.T) {
	got, err := Query(seedStore(t), Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d, want 3", len(got))
	}
	// Newest first: phs000200 (Mar) > patients (Feb) > phs000178 (Jan).
	if got[0].Dataset != "phs000200" || got[2].Dataset != "phs000178" {
		t.Errorf("not sorted newest-first: %s … %s", got[0].Dataset, got[2].Dataset)
	}
}

func TestQuery_FilterByDataClass(t *testing.T) {
	got, _ := Query(seedStore(t), Filter{DataClass: "GENOMIC"})
	if len(got) != 2 {
		t.Fatalf("GENOMIC filter: got %d, want 2", len(got))
	}
	for _, r := range got {
		if r.DataClass != "GENOMIC" {
			t.Errorf("unexpected class %q", r.DataClass)
		}
	}
}

func TestQuery_FilterByDUA(t *testing.T) {
	got, _ := Query(seedStore(t), Filter{DUAID: "DUA-3"})
	if len(got) != 1 || got[0].Dataset != "patients" {
		t.Errorf("DUA filter wrong: %+v", got)
	}
}

func TestQuery_EmptyStore(t *testing.T) {
	got, err := Query(store.New(t.TempDir()), Filter{})
	if err != nil || len(got) != 0 {
		t.Errorf("empty store: want (0,nil), got (%d,%v)", len(got), err)
	}
}
