// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package stewardasp_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/provabl/evidence/asp"
	"github.com/provabl/evidence/cvm"
	"github.com/provabl/evidence/lower"
	"github.com/provabl/evidence/term"

	stewardasp "github.com/provabl/steward/internal/evidence"
	"github.com/provabl/steward/internal/store"
)

// stubSource returns a fixed record (or nil for the missing-record case) for any
// target, so the provider tests run with no store and no filesystem.
type stubSource struct{ rec *store.ProvenanceRecord }

func (s stubSource) Fetch(context.Context, term.Target) (*store.ProvenanceRecord, error) {
	return s.rec, nil
}

// appraise builds the CVM, runs the canonical term, and appraises — the same path
// gate.Evaluate will drive (PR4).
func appraise(t *testing.T, rec *store.ProvenanceRecord, params term.Params) asp.Verdict {
	t.Helper()
	reg := asp.NewRegistry()
	if err := reg.Register(stewardasp.Provider(stubSource{rec: rec})); err != nil {
		t.Fatalf("register: %v", err)
	}
	am, err := stewardasp.NewEphemeralAM()
	if err != nil {
		t.Fatalf("am: %v", err)
	}
	c := cvm.New(reg, am, am, nil)
	protocol := term.Seq(
		term.Nonce(),
		term.Seq(
			term.Meas(term.Self, stewardasp.ID, stewardasp.Target("s3://sre/genomic/phs000178/"), params),
			term.Sig(),
		),
	)
	bundle, ch, err := c.Run(context.Background(), protocol)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	v, err := c.Appraise(context.Background(), bundle, ch)
	if err != nil {
		t.Fatalf("appraise: %v", err)
	}
	return v
}

func verifiedRecord() *store.ProvenanceRecord {
	return &store.ProvenanceRecord{
		Dataset:           "phs000178",
		Dest:              "s3://sre/genomic/phs000178/",
		Source:            "globus:dtn.ncbi.nlm.nih.gov/dbgap/phs000178",
		Digest:            "sha256:abc123",
		IntegrityVerified: true,
		DUAID:             "DUA-2025-001",
		DataClass:         "GENOMIC",
		RecordedAt:        time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
	}
}

func claim(v asp.Verdict, key string) (string, bool) {
	for _, c := range v.Claims {
		if c.Key == key {
			return c.Value, true
		}
	}
	return "", false
}

func TestAppraise_VerifiedRecordPasses(t *testing.T) {
	v := appraise(t, verifiedRecord(), term.Params{"dua_required": "true"})
	if !v.Pass {
		t.Errorf("verified record should pass, got fail: %s", v.Reason)
	}
	if val, _ := claim(v, stewardasp.ClaimProvenanceVerified); val != "true" {
		t.Errorf("ProvenanceVerified = %q, want true", val)
	}
	if val, _ := claim(v, stewardasp.ClaimDUAID); val != "DUA-2025-001" {
		t.Errorf("DUAId = %q, want DUA-2025-001", val)
	}
}

func TestAppraise_UnverifiedDigestFails(t *testing.T) {
	rec := verifiedRecord()
	rec.IntegrityVerified = false // recorded but not yet steward-recomputed
	v := appraise(t, rec, nil)
	if v.Pass {
		t.Error("a record with an unverified digest must NOT pass — provenance means recomputed-and-matched")
	}
	// Posture is still emitted on failure.
	if val, _ := claim(v, stewardasp.ClaimProvenanceVerified); val != "false" {
		t.Errorf("ProvenanceVerified = %q, want false", val)
	}
}

func TestAppraise_MissingDUAWhenRequired(t *testing.T) {
	rec := verifiedRecord()
	rec.DUAID = ""
	v := appraise(t, rec, term.Params{"dua_required": "true"})
	if v.Pass {
		t.Error("dua_required with no DUA must fail")
	}
}

func TestAppraise_WrongDataClass(t *testing.T) {
	v := appraise(t, verifiedRecord(), term.Params{"require_data_class": "PHI"})
	if v.Pass {
		t.Error("require_data_class PHI against a GENOMIC record must fail")
	}
}

func TestAppraise_SourceAllowlist(t *testing.T) {
	// Allowed prefix matches → pass (record is otherwise valid).
	v := appraise(t, verifiedRecord(), term.Params{"allowed_sources": "globus:dtn.ncbi.nlm.nih.gov"})
	if !v.Pass {
		t.Errorf("source on the allowlist should pass: %s", v.Reason)
	}
	if val, _ := claim(v, stewardasp.ClaimSourceVerified); val != "true" {
		t.Errorf("SourceVerified = %q, want true", val)
	}
	// A source not on the list → fail.
	bad := appraise(t, verifiedRecord(), term.Params{"allowed_sources": "globus:other.example.org"})
	if bad.Pass {
		t.Error("source not on the allowlist must fail")
	}
}

func TestAppraise_MissingRecordFailsClosed(t *testing.T) {
	v := appraise(t, nil, nil) // stubSource returns nil → CollectFailed
	if v.Pass {
		t.Error("a missing provenance record must fail closed")
	}
	// The kernel surfaces the un-collected measurement; ProvenanceVerified must not be true.
	if val, ok := claim(v, stewardasp.ClaimProvenanceVerified); ok && val == "true" {
		t.Error("missing record must never report ProvenanceVerified=true")
	}
}

// TestClaimKeysGolden pins the context.data.* claim-key contract attest's PDP
// reads. If this changes, attest's reader and any deployed policy must change in
// lockstep — so the test forces the change to be deliberate.
func TestClaimKeysGolden(t *testing.T) {
	v := appraise(t, verifiedRecord(), nil)
	got := map[string]bool{}
	for _, c := range v.Claims {
		got[c.Key] = true
	}
	want := []string{
		stewardasp.ClaimDataset,
		stewardasp.ClaimProvenanceVerified,
		stewardasp.ClaimSourceVerified,
		stewardasp.ClaimIntegrityChecked,
		stewardasp.ClaimDUAID,
		stewardasp.ClaimDataClass,
		stewardasp.ClaimSubjectDigest,
		stewardasp.ClaimRecordedAt,
	}
	for _, k := range want {
		if !got[k] {
			t.Errorf("missing required claim key %q (context.data.* contract drift)", k)
		}
	}
	// Pin the exact string values too — these are the wire contract.
	expect := map[string]string{
		stewardasp.ClaimDataset:            "data.Dataset",
		stewardasp.ClaimProvenanceVerified: "data.ProvenanceVerified",
		stewardasp.ClaimSourceVerified:     "data.SourceVerified",
		stewardasp.ClaimIntegrityChecked:   "data.IntegrityChecked",
		stewardasp.ClaimDUAID:              "data.DUAId",
		stewardasp.ClaimDataClass:          "data.DataClass",
		stewardasp.ClaimSubjectDigest:      "data.SubjectDigest",
		stewardasp.ClaimRecordedAt:         "data.RecordedAt",
		stewardasp.ClaimFailure:            "data.failure",
	}
	for constName, literal := range expect {
		if constName != literal {
			t.Errorf("claim key drifted: %q != %q", constName, literal)
		}
	}
}

// TestLowerToAttributes confirms the verdict lowers to context.data.* attributes
// plus the overall `attested` boolean attest reads.
func TestLowerToAttributes(t *testing.T) {
	v := appraise(t, verifiedRecord(), nil)
	attrs := lower.ToAttributes(v)
	if attrs["attested"].Value != "true" {
		t.Errorf("attested = %q, want true", attrs["attested"].Value)
	}
	if attrs[stewardasp.ClaimProvenanceVerified].Value != "true" {
		t.Errorf("lowered ProvenanceVerified = %q, want true", attrs[stewardasp.ClaimProvenanceVerified].Value)
	}
	// sanity: keys are sorted/stable enough to enumerate deterministically
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		t.Error("expected lowered attributes")
	}
}
