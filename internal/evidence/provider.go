// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package stewardasp is steward's (ASP, appraiser) pair for the provabl/evidence
// kernel — the data:// provider. It is the data-plane analogue of vet's artifact://
// pair: the gate verdict is produced by running a Copland term through the kernel's
// CVM and appraising the resulting evidence, then lowering to Cedar context.data.*
// attributes attest's PDP reads.
//
// The pair lives in steward (not in evidence/providers) on purpose, exactly as
// vet's does: the kernel's invariant (no ASP-specific branch) constrains only the
// kernel packages, and keeping the claim shape here — next to the store.GateResult
// it feeds — is what keeps steward's Cedar output contract authored in one place.
package stewardasp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/provabl/evidence/asp"
	"github.com/provabl/evidence/ev"
	"github.com/provabl/evidence/term"

	"github.com/provabl/steward/internal/store"
)

// ID keys this pair in the kernel registry.
const ID term.ASPID = "steward"

// TargetScheme is the opaque scheme the kernel routes on; the destination ref is
// carried after it. The kernel never parses past the scheme.
const TargetScheme = "data://"

// Target builds the kernel target for an ingestion destination.
func Target(dest string) term.Target { return term.Target(TargetScheme + dest) }

// destRef recovers the destination from a kernel target.
func destRef(t term.Target) string { return strings.TrimPrefix(string(t), TargetScheme) }

// RecordSource fetches the provenance record for a destination target. Injected
// so the provider tests with no store/filesystem. Returns (nil, nil) when no
// record exists — a fact about the world, not an error.
type RecordSource interface {
	Fetch(ctx context.Context, target term.Target) (*store.ProvenanceRecord, error)
}

// Provider assembles the steward pair from an injected record source.
func Provider(src RecordSource) asp.Provider {
	return asp.Provider{
		ID:        ID,
		Measurer:  measurer{src: src},
		Appraiser: appraiser{},
	}
}

// StoreSource is the production RecordSource: it reads the provenance record from
// the .steward/ store keyed by destination.
type StoreSource struct{ Store *store.Store }

// Fetch loads the record for the destination named by the target.
func (s StoreSource) Fetch(_ context.Context, t term.Target) (*store.ProvenanceRecord, error) {
	return s.Store.LoadRecord(destRef(t))
}

// --- measurer: gather, do not judge -----------------------------------------

type measurer struct{ src RecordSource }

func (m measurer) Measure(ctx context.Context, in asp.MeasureIn) (ev.Measurement, error) {
	rec, err := m.src.Fetch(ctx, in.Target)
	if err != nil {
		return ev.Measurement{}, fmt.Errorf("steward: fetch record: %w", err)
	}
	if rec == nil {
		// No provenance record — a recorded fact, never a pass. The kernel-native
		// expression of gate's fail-closed path (same as vet's missing-record case).
		return ev.Measurement{
			Status: ev.CollectFailed,
			Detail: fmt.Sprintf("no provenance record for %s — run 'steward provenance record' first", destRef(in.Target)),
		}, nil
	}
	// steward does not bind in.Nonce: provenance is recorded-fact evidence with no
	// native challenge channel. Freshness rides the kernel's outer SIG over
	// Seq(Nonce, Meas) — the same reason vet was the kernel's first non-Nitro pair.
	payload, err := json.Marshal(rec)
	if err != nil {
		return ev.Measurement{}, fmt.Errorf("steward: marshal record: %w", err)
	}
	return ev.Measurement{Payload: payload, Status: ev.Collected}, nil
}

// --- appraiser: decode, judge, emit claims ----------------------------------

// Claim keys for the Cedar data contract. These names are the hard contract
// attest's Cedar PDP reads from gate-result.json (context.data.*); they are pinned
// by a golden test so they cannot silently drift.
const (
	ClaimDataset            = "data.Dataset"            // the dataset/study id (e.g. phs000178)
	ClaimProvenanceVerified = "data.ProvenanceVerified" // steward recomputed the digest and it matched
	ClaimSourceVerified     = "data.SourceVerified"     // the source URI is on the allowed list
	ClaimIntegrityChecked   = "data.IntegrityChecked"   // an integrity re-check was performed
	ClaimDUAID              = "data.DUAId"              // governing DUA id
	ClaimDataClass          = "data.DataClass"          // the dataset's data class
	ClaimSubjectDigest      = "data.SubjectDigest"      // the content digest (dataset analogue of workload.ArtifactHash)
	ClaimRecordedAt         = "data.RecordedAt"         // when provenance was recorded (recency signal)
	// ClaimFailure carries one human-readable policy failure. Repeated; the gate
	// reconstructs the bulleted Failures[] from these rather than splitting Reason.
	ClaimFailure = "data.failure"
)

type appraiser struct{}

func (appraiser) Appraise(_ context.Context, in asp.AppraiseIn) (asp.Verdict, error) {
	var rec store.ProvenanceRecord
	if err := json.Unmarshal(in.Meas.Payload, &rec); err != nil {
		return asp.Verdict{}, fmt.Errorf("steward: decode record: %w", err)
	}

	sourceVerified := sourceAllowed(rec.Source, in.Params["allowed_sources"])

	// Posture claims — always emitted, regardless of pass/fail, so a policy can
	// read the data posture even on a denial.
	claims := []asp.Claim{
		{Key: ClaimDataset, Value: rec.Dataset, Type: "string"},
		{Key: ClaimProvenanceVerified, Value: boolStr(rec.IntegrityVerified), Type: "bool"},
		{Key: ClaimSourceVerified, Value: boolStr(sourceVerified), Type: "bool"},
		{Key: ClaimIntegrityChecked, Value: boolStr(rec.IntegrityVerified), Type: "bool"},
		{Key: ClaimDUAID, Value: rec.DUAID, Type: "string"},
		{Key: ClaimDataClass, Value: rec.DataClass, Type: "string"},
		{Key: ClaimSubjectDigest, Value: rec.Digest, Type: "string"},
		{Key: ClaimRecordedAt, Value: rec.RecordedAt.UTC().Format("2006-01-02T15:04:05Z"), Type: "string"},
	}

	// Judgment — params-driven, applied by the kernel via term.Params, never by the
	// gate. A provenance record that hasn't had its digest verified by steward must
	// not pass: the whole point is "recomputed and matched," not "asserted."
	var failures []string
	if !rec.IntegrityVerified {
		failures = append(failures, "provenance digest not verified — run 'steward provenance verify' (recorded digest is only asserted)")
	}
	if in.Params["allowed_sources"] != "" && !sourceVerified {
		failures = append(failures, fmt.Sprintf("source %q is not on the allowed-sources list", rec.Source))
	}
	if in.Params["dua_required"] == "true" && rec.DUAID == "" {
		failures = append(failures, "no governing DUA recorded for this ingestion")
	}
	if want := in.Params["require_data_class"]; want != "" && rec.DataClass != want {
		failures = append(failures, fmt.Sprintf("data class %q does not match required %q", rec.DataClass, want))
	}

	for _, f := range failures {
		claims = append(claims, asp.Claim{Key: ClaimFailure, Value: f, Type: "string"})
	}

	reason := "data ingestion provenance verified"
	if len(failures) > 0 {
		reason = strings.Join(failures, "; ")
	}
	return asp.Verdict{Pass: len(failures) == 0, Claims: claims, Reason: reason}, nil
}

// sourceAllowed reports whether src matches one of the '+'-delimited allowed
// source prefixes (empty allowlist → not constrained → treated as allowed only
// when the gate doesn't require it; the appraiser distinguishes the two via the
// param presence check above).
func sourceAllowed(src, allowed string) bool {
	if allowed == "" {
		return true // no allowlist configured; SourceVerified posture is vacuously true
	}
	if src == "" {
		return false
	}
	for _, a := range strings.Split(allowed, "+") {
		if a != "" && strings.HasPrefix(src, a) {
			return true
		}
	}
	return false
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
