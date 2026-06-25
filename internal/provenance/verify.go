// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package provenance verifies a recorded ingestion's content digest against the
// bytes actually present at the destination. A passing verify is what flips a
// ProvenanceRecord from an *asserted* digest to integrity_verified=true — so the
// gate's ProvenanceVerified claim means "steward recomputed and matched," not
// "a mover told us."
package provenance

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"strings"
)

// ObjectReader opens the bytes at an ingestion destination for re-hashing.
// Injected so verification is testable without AWS/filesystem; the production
// impl (cmd/steward) handles local paths, with S3 deferred to the AWS slice.
type ObjectReader interface {
	Open(ctx context.Context, dest string) (io.ReadCloser, error)
}

// Result is the outcome of a verify.
type Result struct {
	Matched        bool
	ComputedDigest string // "sha256:<hex>"
	RecordedDigest string
}

// Verify recomputes the sha256 of the bytes at dest and compares it to recorded.
// recorded must be a non-empty "sha256:<hex>" digest (the asserted value from
// `provenance record`). Returns Matched=true only when the recomputed digest
// equals the recorded one.
func Verify(ctx context.Context, r ObjectReader, dest, recorded string) (*Result, error) {
	if recorded == "" {
		return nil, fmt.Errorf("no recorded digest to verify against — record one with --digest first")
	}
	rc, err := r.Open(ctx, dest)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", dest, err)
	}
	defer rc.Close()

	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return nil, fmt.Errorf("hash %s: %w", dest, err)
	}
	computed := fmt.Sprintf("sha256:%x", h.Sum(nil))

	return &Result{
		Matched:        strings.EqualFold(computed, recorded),
		ComputedDigest: computed,
		RecordedDigest: recorded,
	}, nil
}
