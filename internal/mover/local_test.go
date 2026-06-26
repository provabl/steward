// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package mover

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalMover_CopiesAndDigests(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.bin")
	dst := filepath.Join(dir, "landing", "dest.bin")
	data := []byte("genomic study bytes")
	if err := os.WriteFile(src, data, 0o600); err != nil {
		t.Fatal(err)
	}

	res, err := NewLocalMover().Move(context.Background(), Request{
		Dataset: "phs1", Source: src, Dest: dst,
	})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}

	// Destination has the bytes.
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != string(data) {
		t.Fatalf("dest content wrong: %q (%v)", got, err)
	}
	// Digest matches the content; mechanism + byte count reported.
	want := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	if res.Digest != want {
		t.Errorf("digest = %s, want %s", res.Digest, want)
	}
	if res.BytesMoved != int64(len(data)) || res.Mechanism != "local" {
		t.Errorf("result = %+v", res)
	}
}

func TestLocalMover_FileURIScheme(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "s.bin")
	_ = os.WriteFile(src, []byte("x"), 0o600)
	dst := filepath.Join(dir, "d.bin")
	// file:// prefixes should be accepted.
	if _, err := NewLocalMover().Move(context.Background(), Request{
		Source: "file://" + src, Dest: "file://" + dst,
	}); err != nil {
		t.Fatalf("file:// move: %v", err)
	}
	if _, err := os.Stat(dst); err != nil {
		t.Errorf("dest not written for file:// move: %v", err)
	}
}

func TestLocalMover_RejectsRemoteScheme(t *testing.T) {
	_, err := NewLocalMover().Move(context.Background(), Request{
		Source: "s3://bucket/key", Dest: "/tmp/x",
	})
	if err == nil {
		t.Error("local mover must reject an s3:// source (needs a scheme-specific mover)")
	}
}

func TestLocalMover_MissingSource(t *testing.T) {
	_, err := NewLocalMover().Move(context.Background(), Request{
		Source: filepath.Join(t.TempDir(), "nope.bin"), Dest: filepath.Join(t.TempDir(), "d.bin"),
	})
	if err == nil {
		t.Error("expected an error opening a missing source")
	}
}
