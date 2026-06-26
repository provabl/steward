// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package closeout drives the destruction-or-retention bookend of the
// move-to-compute lifecycle (spec §5): at DUA closeout, verify the retention term
// has elapsed, destroy the data, and emit an auditable destruction certificate.
//
// This is the highest-consequence operation in steward — it DELETES controlled
// data — so its design is "certify + require explicit confirmation, never silent
// delete" (ADR 0004), mirroring attest approval's "no activation without a
// recorded basis." The Closer enforces three gates BEFORE anything is destroyed:
//
//  1. A provenance record must exist for the destination (you can only close out
//     data steward governs).
//  2. Any Object Lock retention must have fully ELAPSED — destroying before the
//     DUA term ends would itself violate the term. Fail-closed: if the retention
//     state can't be read, nothing is destroyed.
//  3. The caller must explicitly CONFIRM (Confirm=true) and name the destroying
//     principal — a dry run (the default) reports what *would* happen and destroys
//     nothing.
//
// Only then does it call the Destroyer (the live S3 deletion) and write a
// DestructionCertificate. The destroyer is a seam; the gates are the product.
package closeout

import (
	"context"
	"fmt"
	"time"

	"github.com/provabl/steward/internal/store"
)

// State is what the Destroyer reports about a destination before destruction.
type State struct {
	// Objects is how many objects live under the destination prefix.
	Objects int
	// EarliestRetainUntil is the soonest Object Lock retain-until across those
	// objects (nil if none carry a retention). Destruction is refused until this
	// has passed — the least-locked object still gates the whole prefix.
	EarliestRetainUntil *time.Time
}

// Destroyer reads a destination's pre-destruction state and destroys it. The live
// impl is S3 (list + delete every object/version); a fake drives the Closer tests.
// Destroy must only be called by the Closer after the gates pass.
type Destroyer interface {
	// Name identifies the destroyer (e.g. "s3").
	Name() string
	// State reports the object count + earliest retention at dest.
	State(ctx context.Context, dest string) (State, error)
	// Destroy deletes everything under dest and returns how many objects it removed.
	Destroy(ctx context.Context, dest string) (objectsDestroyed int, err error)
}

// Request is one closeout request.
type Request struct {
	Dataset   string
	DUAID     string
	Confirm   bool   // false = dry run (report only, destroy nothing)
	Principal string // who is confirming the destruction (required when Confirm)
}

// Outcome reports what a closeout did (or would do, on a dry run).
type Outcome struct {
	DryRun           bool
	Dest             string
	Objects          int                           // objects present (dry run) / destroyed (live)
	RetentionCleared bool                          // the retention term had elapsed (or there was none)
	Certificate      *store.DestructionCertificate // written only on a confirmed destruction
}

// Closer orchestrates closeout over a Destroyer + the store.
type Closer struct {
	destroyer Destroyer
	store     *store.Store
	now       func() time.Time
}

// New builds a Closer.
func New(d Destroyer, s *store.Store) *Closer {
	return &Closer{destroyer: d, store: s, now: time.Now}
}

// WithClock overrides the clock (tests).
func (c *Closer) WithClock(now func() time.Time) *Closer { c.now = now; return c }

// Closeout runs the gated destruction for the dataset's destination. By default
// (Confirm=false) it is a DRY RUN: it checks the gates and reports what would be
// destroyed, touching nothing. With Confirm=true and a Principal it destroys the
// data and writes a DestructionCertificate. It is fail-closed: a missing
// provenance record, an unelapsed retention, an unreadable state, or a confirmed
// request without a principal all stop before any deletion.
func (c *Closer) Closeout(ctx context.Context, dest string, req Request) (*Outcome, error) {
	if dest == "" {
		return nil, fmt.Errorf("dest is required")
	}

	// Gate 1: only close out data steward governs.
	rec, err := c.store.LoadRecord(dest)
	if err != nil {
		return nil, fmt.Errorf("load provenance record: %w", err)
	}
	if rec == nil {
		return nil, fmt.Errorf("no provenance record for %s — steward only closes out data it governs", dest)
	}
	if req.DUAID != "" && rec.DUAID != req.DUAID {
		return nil, fmt.Errorf("DUA mismatch: record is governed by %q, not %q", rec.DUAID, req.DUAID)
	}

	// Gate 2: retention must have elapsed. Fail-closed on an unreadable state.
	st, err := c.destroyer.State(ctx, dest)
	if err != nil {
		return nil, fmt.Errorf("read destruction state at %s (fail-closed): %w", dest, err)
	}
	now := c.now()
	if st.EarliestRetainUntil != nil && st.EarliestRetainUntil.After(now) {
		return nil, fmt.Errorf("refusing to destroy %s: Object Lock retention runs until %s (%s remaining) — the DUA term has not elapsed",
			dest, st.EarliestRetainUntil.Format(time.RFC3339), st.EarliestRetainUntil.Sub(now).Truncate(time.Second))
	}

	out := &Outcome{Dest: dest, Objects: st.Objects, RetentionCleared: true}

	// Gate 3: a dry run (the default) destroys nothing.
	if !req.Confirm {
		out.DryRun = true
		return out, nil
	}
	if req.Principal == "" {
		return nil, fmt.Errorf("--principal is required to confirm destruction (who authorizes it)")
	}

	// All gates passed: destroy, then certify.
	destroyed, err := c.destroyer.Destroy(ctx, dest)
	if err != nil {
		return nil, fmt.Errorf("destroy %s: %w", dest, err)
	}
	out.Objects = destroyed

	cert := &store.DestructionCertificate{
		Dataset:          rec.Dataset,
		Dest:             dest,
		DUAID:            rec.DUAID,
		DataClass:        rec.DataClass,
		Digest:           rec.Digest,
		ObjectsDestroyed: destroyed,
		DestroyedBy:      req.Principal,
		DestroyedAt:      now.UTC(),
	}
	if st.EarliestRetainUntil != nil {
		cert.RetainedUntil = st.EarliestRetainUntil.UTC()
	}
	if err := c.store.SaveCertificate(cert); err != nil {
		return nil, fmt.Errorf("write destruction certificate: %w", err)
	}
	out.Certificate = cert
	return out, nil
}
