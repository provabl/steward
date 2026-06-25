// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package mover defines the pluggable-transport seam for steward's move-to-compute
// ingestion. The mover is the part that actually copies bytes into the SRE (Globus
// High-Assurance, AWS DataSync, `aws s3 cp`); steward wraps that movement with the
// governance the suite was missing — an authorization check before, a recorded
// provenance record during, handling/retention after.
//
// This package is the INTERFACE ONLY in v1: there are no live movers. v1 governs
// data that was moved out-of-band — the operator runs `steward provenance record`
// after the fact. The interface is defined now so the deferred `steward ingest`
// command (spec §1) has a stable seam to plug Globus / DataSync / s3cp impls into,
// and so the design center — "the mover is pluggable; the governance is the product"
// (spec §Architecture) — is expressed in code, not just prose.
//
// Design note: steward must never silently move bytes. A Mover only transports; the
// authorization check, provenance recording, and handling application stay in
// steward's own flow (the deferred internal/ingest), which calls Move only after the
// DUA/source/data-class preconditions pass.
package mover

import "context"

// Request describes one ingestion transfer. It is transport-agnostic; a concrete
// Mover interprets Source/Dest in its own scheme (e.g. a Globus endpoint:path, an
// s3:// URI). Steward derives the provenance record from the Result, not from the
// mover's internal state.
type Request struct {
	Dataset string // study/dataset id, e.g. "phs000178"
	Source  string // mover-scheme source, e.g. "globus:dtn.ncbi.nlm.nih.gov/dbgap/phs000178"
	Dest    string // destination the bytes land at, e.g. "s3://sre-genomic/dbgap/phs000178/"
}

// Result is what a Mover reports after a transfer. Digest is the content digest the
// mover observed; steward treats it as ASSERTED — `steward provenance verify`
// recomputes it against Dest before it counts as integrity-verified (the
// recomputed-and-matched rule). BytesMoved/Mechanism feed the provenance record.
type Result struct {
	Digest     string // observed content digest (sha256:...), asserted — not yet steward-verified
	BytesMoved int64
	Mechanism  string // transport label recorded as ProvenanceRecord.Mover, e.g. "globus" | "datasync" | "s3cp"
}

// Mover transports bytes into the SRE. Implementations are deferred (Globus,
// DataSync, s3cp); v1 ships the interface only. Move must be context-cancellable and
// must not apply any governance (tagging, locking, authorization) — that is
// steward's job, not the transport's.
type Mover interface {
	// Name is the transport label recorded in provenance (e.g. "globus").
	Name() string
	// Move transports the data described by req and reports the result. It must not
	// be called until steward has verified the ingestion is authorized.
	Move(ctx context.Context, req Request) (*Result, error)
}
