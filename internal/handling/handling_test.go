// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package handling

import (
	"context"
	"testing"
	"time"
)

// fakeTagger proves the Tagger seam is implementable and usable — the contract the
// deferred S3 Object Lock / retention impl will fill.
type fakeTagger struct{ got Spec }

func (f *fakeTagger) Apply(_ context.Context, spec Spec) error {
	f.got = spec
	return nil
}

func TestTagger_SeamUsable(t *testing.T) {
	until := time.Date(2027, 5, 1, 0, 0, 0, 0, time.UTC)
	f := &fakeTagger{}
	var tg Tagger = f
	err := tg.Apply(context.Background(), Spec{
		Dest:        "s3://sre-genomic/dbgap/phs000178/",
		DataClass:   "GENOMIC",
		DUAID:       "DUA-2025-001",
		RetainUntil: &until,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if f.got.DataClass != "GENOMIC" || f.got.RetainUntil == nil || !f.got.RetainUntil.Equal(until) {
		t.Errorf("spec not passed through: %+v", f.got)
	}
}
