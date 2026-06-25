// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package mover

import (
	"context"
	"testing"
)

// fakeMover proves the Mover interface is implementable and usable — the seam the
// deferred Globus/DataSync/s3cp impls will fill.
type fakeMover struct{ digest string }

func (fakeMover) Name() string { return "fake" }

func (f fakeMover) Move(_ context.Context, req Request) (*Result, error) {
	return &Result{Digest: f.digest, BytesMoved: 42, Mechanism: "fake"}, nil
}

func TestMover_SeamUsable(t *testing.T) {
	var m Mover = fakeMover{digest: "sha256:abc"}
	if m.Name() != "fake" {
		t.Errorf("Name() = %q", m.Name())
	}
	res, err := m.Move(context.Background(), Request{Dataset: "phs000178", Source: "fake:src", Dest: "s3://d/"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	// The mover-observed digest is asserted; provenance verify recomputes it.
	if res.Digest != "sha256:abc" || res.Mechanism != "fake" {
		t.Errorf("result = %+v", res)
	}
}
