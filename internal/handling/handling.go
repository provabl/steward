// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package handling defines the seam for applying a data class's storage controls to
// an ingested destination: the data-class tag, S3 Object Lock / retention for the
// DUA term, and an expiry aligned to the approval. This is what makes "data was
// brought in *and handled correctly*" provable rather than hoped (spec §3,
// `steward apply-handling`).
//
// This package is the INTERFACE ONLY in v1. Applying Object Lock / retention is
// high-consequence and needs live AWS (it cannot be exercised in CI without real
// resources), so the impl is a deferred follow-on. The Tagger seam is defined now so
// the deferred `steward apply-handling` command has a stable contract, and so the
// boundary is explicit: steward applies handling; it never decides access (attest
// does) and never destroys data outside the separate, certify-and-confirm closeout
// path.
package handling

import (
	"context"
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

// Tagger applies handling controls to a destination. The sole v1 implementation is
// a test fake; the live S3 impl (PutObjectTagging + Object Lock retention) is
// deferred. Apply must be idempotent — re-running it for the same Spec is safe — and
// must never RELAX an existing control (extending retention is allowed; shortening
// it is not, since that would let an operator defeat a DUA term).
type Tagger interface {
	// Apply writes the data-class tag and, when Spec.RetainUntil is set, the Object
	// Lock retention. It does not move or read object bytes.
	Apply(ctx context.Context, spec Spec) error
}
