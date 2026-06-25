// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package gate evaluates a data-ingestion provenance record against a policy and
// writes the Cedar data attribute result to .steward/gate-result.json. attest's
// Cedar PDP reads context.data.* attributes from this file — the data-plane
// counterpart to vet's context.workload.*.
package gate

import (
	"context"
	"fmt"
	"time"

	"github.com/provabl/evidence/asp"
	"github.com/provabl/evidence/cvm"
	"github.com/provabl/evidence/lower"
	"github.com/provabl/evidence/term"

	stewardasp "github.com/provabl/steward/internal/evidence"
	"github.com/provabl/steward/internal/store"
)

// Policy is steward's data-ingestion gate policy. It flows to the appraiser as
// term.Params, so the kernel — not gate — applies the rules.
type Policy struct {
	// DUARequired fails the gate when the record has no governing DUA.
	DUARequired bool
	// RequireDataClass, when set, requires the record's data class to match.
	RequireDataClass string
	// AllowedSources is a '+'-delimited list of allowed source-URI prefixes; when
	// set, a record whose source matches none fails.
	AllowedSources string
}

// DefaultPolicy requires a verified digest (always enforced by the appraiser) and
// a governing DUA — the floor for controlled-data ingestion.
func DefaultPolicy() *Policy { return &Policy{DUARequired: true} }

// Evaluator runs provenance records through the kernel against a policy.
type Evaluator struct {
	store  *store.Store
	policy *Policy
}

// New creates an Evaluator.
func New(s *store.Store, p *Policy) *Evaluator {
	if p == nil {
		p = DefaultPolicy()
	}
	return &Evaluator{store: s, policy: p}
}

// EvaluateResult is the outcome of a gate evaluation.
type EvaluateResult struct {
	Dest          string
	PolicyMet     bool
	MissingRecord bool // true when no provenance record exists for the destination
	Failures      []string
	GateResult    *store.GateResult
}

// Evaluate looks up the provenance record for dest and evaluates it against the
// policy THROUGH the provabl/evidence kernel: it runs the canonical Copland term
// Seq(Nonce, Seq(Meas, Sig)) through the CVM, appraises the bundle, and lowers the
// verdict to the context.data.* attributes written to gate-result.json. The
// judgment lives in steward's (ASP, appraiser) pair, not here.
//
// If no provenance record exists, the measurer returns CollectFailed; appraisal
// fails and Evaluate writes a fail-closed gate result (PolicyMet=false, attributes
// false/empty) with MissingRecord=true so the caller can print guidance.
func (e *Evaluator) Evaluate(ctx context.Context, dest string) (*EvaluateResult, error) {
	reg := asp.NewRegistry()
	if err := reg.Register(stewardasp.Provider(stewardasp.StoreSource{Store: e.store})); err != nil {
		return nil, fmt.Errorf("register steward provider: %w", err)
	}
	am, err := stewardasp.NewEphemeralAM()
	if err != nil {
		return nil, err
	}
	c := cvm.New(reg, am, am, nil)

	params := term.Params{}
	if e.policy.DUARequired {
		params["dua_required"] = "true"
	}
	if e.policy.RequireDataClass != "" {
		params["require_data_class"] = e.policy.RequireDataClass
	}
	if e.policy.AllowedSources != "" {
		params["allowed_sources"] = e.policy.AllowedSources
	}

	protocol := term.Seq(
		term.Nonce(),
		term.Seq(
			term.Meas(term.Self, stewardasp.ID, stewardasp.Target(dest), params),
			term.Sig(),
		),
	)

	bundle, ch, err := c.Run(ctx, protocol)
	if err != nil {
		return nil, fmt.Errorf("run attestation: %w", err)
	}
	verdict, err := c.Appraise(ctx, bundle, ch)
	if err != nil {
		return nil, fmt.Errorf("appraise: %w", err)
	}

	attrs := lower.ToAttributes(verdict)
	missingRecord := isMissingRecord(verdict)
	gateResult := gateResultFromAttrs(dest, attrs, verdict.Pass)

	if missingRecord {
		_ = e.store.SaveGateResult(gateResult) // best-effort; don't mask the guidance
	} else if err := e.store.SaveGateResult(gateResult); err != nil {
		return nil, fmt.Errorf("save gate result: %w", err)
	}

	failures := failuresFromVerdict(verdict)
	if missingRecord {
		failures = []string{"no provenance record — run 'steward provenance record' before 'steward gate'"}
	}

	return &EvaluateResult{
		Dest:          dest,
		PolicyMet:     verdict.Pass,
		MissingRecord: missingRecord,
		Failures:      failures,
		GateResult:    gateResult,
	}, nil
}

// isMissingRecord reports whether the bundle's only finding was that the
// measurement could not be taken (CollectFailed) — surfaced by the kernel as a
// "<asp>.collected=false" claim.
func isMissingRecord(v asp.Verdict) bool {
	for _, c := range v.Claims {
		if c.Key == string(stewardasp.ID)+".collected" && c.Value == "false" {
			return true
		}
	}
	return false
}

// failuresFromVerdict reconstructs the bulleted failure list from the structured
// failure claims the appraiser emitted (not by splitting Reason).
func failuresFromVerdict(v asp.Verdict) []string {
	var out []string
	for _, c := range v.Claims {
		if c.Key == stewardasp.ClaimFailure {
			out = append(out, c.Value)
		}
	}
	return out
}

// gateResultFromAttrs is the single chokepoint mapping lowered Cedar attributes
// back to the store.GateResult contract. Absent data.* attributes (the
// CollectFailed / missing-record case) default to zero/false — the fail-closed result.
func gateResultFromAttrs(dest string, attrs map[string]lower.Attr, policyMet bool) *store.GateResult {
	return &store.GateResult{
		Dataset:            attrs[stewardasp.ClaimDataset].Value,
		Dest:               dest,
		ProvenanceVerified: attrBool(attrs, stewardasp.ClaimProvenanceVerified),
		SourceVerified:     attrBool(attrs, stewardasp.ClaimSourceVerified),
		IntegrityChecked:   attrBool(attrs, stewardasp.ClaimIntegrityChecked),
		DUAID:              attrs[stewardasp.ClaimDUAID].Value,
		DataClass:          attrs[stewardasp.ClaimDataClass].Value,
		Digest:             attrs[stewardasp.ClaimSubjectDigest].Value,
		PolicyMet:          policyMet,
		EvaluatedAt:        time.Now(),
	}
}

func attrBool(attrs map[string]lower.Attr, key string) bool {
	return attrs[key].Value == "true"
}
