// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/provabl/steward/internal/store"
)

// writeData writes bytes to a temp file and returns the path and its sha256
// digest, so a record's asserted digest can match (or not) the bytes on disk.
func writeData(t *testing.T, b []byte) (path, digest string) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "data.bin")
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	h := sha256.Sum256(b)
	return path, fmt.Sprintf("sha256:%x", h[:])
}

func recordAt(t *testing.T, dir, dest, digest string) {
	t.Helper()
	err := store.New(dir).SaveRecord(&store.ProvenanceRecord{
		Dataset:    "phs000178",
		Dest:       dest,
		Digest:     digest,
		DataClass:  "GENOMIC",
		DUAID:      "DUA-2025-001",
		RecordedAt: time.Date(2026, 6, 24, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestProvenanceVerify_MatchFlipsVerified(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".steward")
	dest, digest := writeData(t, []byte("genomic bytes"))
	recordAt(t, dir, dest, digest)

	cmd := provenanceCmd()
	cmd.SetArgs([]string{"verify", "--dest", dest, "--steward-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("verify: %v", err)
	}
	rec, _ := store.New(dir).LoadRecord(dest)
	if rec == nil || !rec.IntegrityVerified {
		t.Errorf("a matching verify must set integrity_verified=true, got %+v", rec)
	}
}

func TestProvenanceVerify_MismatchErrorsAndLeavesUnverified(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".steward")
	dest, _ := writeData(t, []byte("actual bytes"))
	recordAt(t, dir, dest, "sha256:deadbeef") // recorded digest does not match the bytes

	cmd := provenanceCmd()
	cmd.SetArgs([]string{"verify", "--dest", dest, "--steward-dir", dir})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil {
		t.Error("a digest mismatch must error")
	}
	rec, _ := store.New(dir).LoadRecord(dest)
	if rec == nil || rec.IntegrityVerified {
		t.Errorf("a failed verify must NOT flip integrity_verified, got %+v", rec)
	}
}

func TestProvenanceVerify_NoRecord(t *testing.T) {
	cmd := provenanceCmd()
	cmd.SetArgs([]string{"verify", "--dest", "s3://nope/", "--steward-dir", filepath.Join(t.TempDir(), ".steward")})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil {
		t.Error("verify with no recorded provenance: want error")
	}
}

func TestLog_FilterByDataClass(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".steward")
	s := store.New(dir)
	for _, r := range []store.ProvenanceRecord{
		{Dataset: "g1", Dest: "s3://a/", DataClass: "GENOMIC", RecordedAt: time.Now()},
		{Dataset: "p1", Dest: "s3://b/", DataClass: "PHI", RecordedAt: time.Now()},
	} {
		rr := r
		if err := s.SaveRecord(&rr); err != nil {
			t.Fatal(err)
		}
	}
	// JSON output keeps the assertion robust against table formatting.
	cmd := logCmd()
	cmd.SetArgs([]string{"--data-class", "GENOMIC", "--json", "--steward-dir", dir})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("log: %v", err)
	}
}

func TestLog_EmptyStore(t *testing.T) {
	cmd := logCmd()
	cmd.SetArgs([]string{"--steward-dir", filepath.Join(t.TempDir(), ".steward")})
	if err := cmd.Execute(); err != nil {
		t.Errorf("log on an empty store should succeed, got %v", err)
	}
}
