// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package handling applies a data class's storage controls to an ingested
// destination: the data-class tag, and S3 Object Lock / retention for the DUA
// term (spec §3, `steward apply-handling`). This is what makes "data was brought
// in *and handled correctly*" provable rather than hoped.
//
// The package separates the bug-prone GOVERNANCE LOGIC from the live AWS calls:
//
//   - Applier (here) validates the Spec and enforces the load-bearing invariant —
//     **handling may be strengthened but never relaxed**: extending an Object Lock
//     retention is allowed, shortening (or removing) one is not, because that would
//     let an operator defeat a DUA's retention term. This logic is fully testable.
//   - Backend (a seam) reads the destination's current handling and applies a new
//     one. The live impl is S3 (GetObjectTagging/Retention + PutObjectTagging +
//     Object Lock retention); a fake drives the Applier tests with no AWS.
//
// Boundary: steward applies handling; it never decides access (attest does) and
// never destroys data outside the separate, certify-and-confirm closeout path.
package handling

import (
	"context"
	"fmt"
	"time"
)

// Spec is the handling to apply to a destination prefix, derived from the data
// class and the governing DUA's term.
type Spec struct {
	Dest        string     // destination prefix, e.g. "s3://sre-genomic/dbgap/phs000178/"
	DataClass   string     // e.g. "GENOMIC" — written as the data-class tag
	DUAID       string     // governing DUA, recorded alongside the retention lock
	RetainUntil *time.Time // Object Lock retain-until, aligned to the approval expiry; nil = no retention lock
}

// Current is the handling already applied to a destination, as read from the
// Backend. Zero value (DataClass "" and RetainUntil nil) means "no handling yet".
type Current struct {
	DataClass   string
	RetainUntil *time.Time
}

// Backend reads and applies handling at a destination. The live impl is S3; a
// fake drives the Applier tests. Apply must be idempotent (re-applying the same
// Spec is safe) and must only ever set the tag + retention it is given — the
// no-relax decision is the Applier's job, made before Apply is called.
type Backend interface {
	// Name identifies the backend (e.g. "s3").
	Name() string
	// Current returns the handling already applied at dest (zero value if none).
	Current(ctx context.Context, dest string) (Current, error)
	// Apply writes the data-class tag and, when spec.RetainUntil is set, the
	// Object Lock retention. It does not move or read object bytes.
	Apply(ctx context.Context, spec Spec) error
}

// Tagger is retained for compatibility with the v1 seam (a Backend is a superset).
// New code should use Backend; this stays so existing references don't break.
type Tagger interface {
	Apply(ctx context.Context, spec Spec) error
}

// Applier orchestrates apply-handling over a Backend, enforcing validation and the
// no-relax invariant before any control is written.
type Applier struct {
	backend Backend
}

// New builds an Applier over a Backend.
func New(backend Backend) *Applier { return &Applier{backend: backend} }

// Result reports what Apply did.
type Result struct {
	Dest         string
	DataClass    string
	RetainUntil  *time.Time
	RetentionSet bool // whether a retention lock was requested
	Backend      string
}

// Apply validates the spec, refuses to relax existing handling, and applies it via
// the backend. Fail-closed: a backend that cannot read the current state, or a
// spec that would *weaken* an existing retention, is an error — nothing is written.
func (a *Applier) Apply(ctx context.Context, spec Spec) (*Result, error) {
	if spec.Dest == "" || spec.DataClass == "" {
		return nil, fmt.Errorf("dest and data-class are required")
	}
	if spec.RetainUntil != nil && !spec.RetainUntil.After(time.Now()) {
		return nil, fmt.Errorf("retain-until %s is not in the future", spec.RetainUntil.Format(time.RFC3339))
	}

	cur, err := a.backend.Current(ctx, spec.Dest)
	if err != nil {
		return nil, fmt.Errorf("read current handling at %s (fail-closed): %w", spec.Dest, err)
	}
	// No-relax: a new retention must not be earlier than (or drop) an existing one.
	if cur.RetainUntil != nil {
		if spec.RetainUntil == nil {
			return nil, fmt.Errorf("refusing to remove an existing Object Lock retention (until %s) — handling may be strengthened, never relaxed",
				cur.RetainUntil.Format(time.RFC3339))
		}
		if spec.RetainUntil.Before(*cur.RetainUntil) {
			return nil, fmt.Errorf("refusing to shorten Object Lock retention from %s to %s — a DUA term cannot be weakened",
				cur.RetainUntil.Format(time.RFC3339), spec.RetainUntil.Format(time.RFC3339))
		}
	}

	if err := a.backend.Apply(ctx, spec); err != nil {
		return nil, fmt.Errorf("apply handling at %s: %w", spec.Dest, err)
	}
	return &Result{
		Dest:         spec.Dest,
		DataClass:    spec.DataClass,
		RetainUntil:  spec.RetainUntil,
		RetentionSet: spec.RetainUntil != nil,
		Backend:      a.backend.Name(),
	}, nil
}
