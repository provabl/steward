// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package ingest orchestrates authorized move-to-compute ingestion (spec §1): it
// verifies an ingestion is permitted *before* any bytes move, drives the
// configured mover, and records the provenance the rest of steward governs.
//
// The order is the whole point — **authorize, then move, then record**:
//
//	Authorize(req)  → fail-closed if the principal/DUA/source isn't permitted
//	  Mover.Move(req) → the pluggable transport actually copies the bytes
//	    store.SaveRecord → a ProvenanceRecord (integrity_verified=false; the digest
//	                       the mover observed is ASSERTED until `provenance verify`
//	                       recomputes it against the destination)
//
// steward never moves bytes itself and never trusts the mover's digest: the Mover
// is a seam (Globus / DataSync / s3cp / a local reference impl), and the recorded
// digest stays asserted until verified. This package is the governance wrapper the
// suite was missing; the transport is pluggable, the governance is the product.
package ingest

import (
	"context"
	"fmt"
	"time"

	"github.com/provabl/steward/internal/mover"
	"github.com/provabl/steward/internal/store"
)

// Request is one authorized-ingestion request.
type Request struct {
	Dataset   string // study/dataset id, e.g. "phs000178"
	Source    string // mover-scheme source, e.g. "globus:dtn.ncbi.nlm.nih.gov/dbgap/phs000178"
	Dest      string // destination the bytes land at, e.g. "s3://sre-genomic/dbgap/phs000178/"
	DUAID     string // governing Data Use Agreement
	DataClass string // e.g. "GENOMIC"
	Principal string // the requesting principal (ARN), checked by the Authorizer
}

// Decision is an authorization outcome. Permitted=false carries a Reason the CLI
// surfaces; an Authorizer that cannot evaluate returns an error (fail-closed).
type Decision struct {
	Permitted bool
	Reason    string
}

// Authorizer decides whether an ingestion may proceed — *before* any bytes move.
// It answers the spec §1 questions: does the principal hold the dataset's DUA, is
// the source on the allowed list, does the destination carry the right data-class
// posture. Implementations: a config-driven one (v1), and later an IAM-tag one
// that reads attest:nih-dua-ids (the same set the compute-to-data chain checks).
//
// An Authorizer must fail closed: when it cannot reach the facts it needs, it
// returns an error, and the ingestion does not proceed.
type Authorizer interface {
	Authorize(ctx context.Context, req Request) (Decision, error)
}

// Ingester composes an Authorizer, a Mover, and the store into the spec §1 flow.
type Ingester struct {
	auth  Authorizer
	mover mover.Mover
	store *store.Store
	// now is overridable in tests for a deterministic RecordedAt.
	now func() time.Time
}

// New builds an Ingester.
func New(auth Authorizer, mv mover.Mover, s *store.Store) *Ingester {
	return &Ingester{auth: auth, mover: mv, store: s, now: time.Now}
}

// WithClock overrides the timestamp source (tests).
func (i *Ingester) WithClock(now func() time.Time) *Ingester {
	i.now = now
	return i
}

// Result reports what an Ingest did.
type Result struct {
	Record     *store.ProvenanceRecord
	BytesMoved int64
}

// Ingest runs the authorize → move → record flow. It is fail-closed at every
// step: an unauthorized request never moves bytes; a mover error never writes a
// record; and the recorded digest is the mover's *asserted* value with
// integrity_verified=false — `steward provenance verify` must recompute it before
// the gate will pass. Returns an error (and writes nothing) on denial or failure.
func (i *Ingester) Ingest(ctx context.Context, req Request) (*Result, error) {
	if req.Dataset == "" || req.Source == "" || req.Dest == "" {
		return nil, fmt.Errorf("dataset, source, and dest are required")
	}

	// 1. Authorize BEFORE any bytes move.
	dec, err := i.auth.Authorize(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("authorization check failed (fail-closed): %w", err)
	}
	if !dec.Permitted {
		return nil, fmt.Errorf("ingestion not authorized: %s", dec.Reason)
	}

	// 2. Drive the configured mover.
	mres, err := i.mover.Move(ctx, mover.Request{
		Dataset: req.Dataset, Source: req.Source, Dest: req.Dest,
	})
	if err != nil {
		return nil, fmt.Errorf("mover %q: %w", i.mover.Name(), err)
	}

	// 3. Record provenance. The mover's digest is ASSERTED — integrity_verified
	//    stays false until `provenance verify` recomputes it against the dest.
	rec := &store.ProvenanceRecord{
		Dataset:           req.Dataset,
		Dest:              req.Dest,
		Source:            req.Source,
		Digest:            mres.Digest,
		IntegrityVerified: false,
		DUAID:             req.DUAID,
		DataClass:         req.DataClass,
		AuthorizedBy:      req.Principal,
		Mover:             mres.Mechanism,
		RecordedAt:        i.now().UTC(),
	}
	if err := i.store.SaveRecord(rec); err != nil {
		return nil, fmt.Errorf("record provenance: %w", err)
	}
	return &Result{Record: rec, BytesMoved: mres.BytesMoved}, nil
}
