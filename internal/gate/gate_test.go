// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package gate

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/provabl/steward/internal/store"
)

func seed(t *testing.T, verified bool) (*store.Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), ".steward")
	s := store.New(dir)
	dest := "s3://sre/genomic/phs000178/"
	rec := &store.ProvenanceRecord{
		Dataset:           "phs000178",
		Dest:              dest,
		Source:            "globus:dtn/dbgap/phs000178",
		Digest:            "sha256:abc",
		IntegrityVerified: verified,
		DUAID:             "DUA-2025-001",
		DataClass:         "GENOMIC",
		RecordedAt:        time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
	}
	if err := s.SaveRecord(rec); err != nil {
		t.Fatal(err)
	}
	return s, dest
}

func TestEvaluate_VerifiedRecordPasses(t *testing.T) {
	s, dest := seed(t, true)
	res, err := New(s, DefaultPolicy()).Evaluate(context.Background(), dest)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !res.PolicyMet {
		t.Errorf("verified record should pass; failures=%v", res.Failures)
	}
	if res.MissingRecord {
		t.Error("MissingRecord should be false for an existing record")
	}
	// gate-result.json written with the data.* posture.
	g := readGateResult(t, s.Dir())
	if !g.ProvenanceVerified || !g.PolicyMet || g.DUAID != "DUA-2025-001" || g.Dataset != "phs000178" {
		t.Errorf("gate-result wrong: %+v", g)
	}
}

func TestEvaluate_UnverifiedDigestFailsClosed(t *testing.T) {
	s, dest := seed(t, false) // recorded but digest not steward-verified
	res, err := New(s, DefaultPolicy()).Evaluate(context.Background(), dest)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.PolicyMet {
		t.Error("unverified digest must not pass")
	}
	if len(res.Failures) == 0 {
		t.Error("expected a failure explaining the unverified digest")
	}
	g := readGateResult(t, s.Dir())
	if g.PolicyMet || g.ProvenanceVerified {
		t.Errorf("gate-result should be fail-closed: %+v", g)
	}
}

func TestEvaluate_MissingRecordFailsClosed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".steward")
	s := store.New(dir)
	res, err := New(s, DefaultPolicy()).Evaluate(context.Background(), "s3://sre/none/")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.PolicyMet {
		t.Error("missing record must fail closed")
	}
	if !res.MissingRecord {
		t.Error("MissingRecord should be true")
	}
	if len(res.Failures) != 1 || res.Failures[0] == "" {
		t.Errorf("expected the missing-record guidance, got %v", res.Failures)
	}
	// Even fail-closed, the gate-result.json is written (CI/pipelines expect it).
	g := readGateResult(t, s.Dir())
	if g.PolicyMet {
		t.Error("missing-record gate-result must have PolicyMet=false")
	}
}

func TestEvaluate_WrongDataClassFails(t *testing.T) {
	s, dest := seed(t, true)
	res, _ := New(s, &Policy{DUARequired: true, RequireDataClass: "PHI"}).Evaluate(context.Background(), dest)
	if res.PolicyMet {
		t.Error("require_data_class PHI against a GENOMIC record must fail")
	}
}

func readGateResult(t *testing.T, dir string) store.GateResult {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "gate-result.json"))
	if err != nil {
		t.Fatalf("read gate-result.json: %v", err)
	}
	var g store.GateResult
	if err := json.Unmarshal(data, &g); err != nil {
		t.Fatalf("parse gate-result.json: %v", err)
	}
	return g
}
