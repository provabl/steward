// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/provabl/steward/internal/store"
)

// TestIngestCmd_EndToEnd drives the CLI: an authorized ingest copies the file and
// writes a provenance record (asserted digest, unverified).
func TestIngestCmd_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.tar")
	dst := filepath.Join(dir, "sre", "phs1.tar")
	stewardDir := filepath.Join(dir, ".steward")
	if err := os.WriteFile(src, []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := ingestCmd()
	cmd.SetArgs([]string{
		"--dataset", "phs1", "--source", src, "--dest", dst,
		"--dua-id", "DUA-1", "--data-class", "GENOMIC", "--principal", "arn:role/A",
		"--allowed-dua", "DUA-1", "--allowed-source", dir + "/",
		"--require-data-class", "GENOMIC", "--steward-dir", stewardDir,
	})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ingest: %v", err)
	}

	if _, err := os.Stat(dst); err != nil {
		t.Errorf("destination not written: %v", err)
	}
	rec, err := store.New(stewardDir).LoadRecord(dst)
	if err != nil || rec == nil {
		t.Fatalf("expected a provenance record, got (%v, %v)", rec, err)
	}
	if rec.IntegrityVerified {
		t.Error("ingest must record an asserted (unverified) digest")
	}
	if rec.DUAID != "DUA-1" || rec.Mover != "local" {
		t.Errorf("record fields wrong: %+v", rec)
	}
}

// TestIngestCmd_DeniedNoMove: a DUA not on the allow-list is denied; nothing moves.
func TestIngestCmd_DeniedNoMove(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "in.tar")
	dst := filepath.Join(dir, "out.tar")
	_ = os.WriteFile(src, []byte("data"), 0o600)

	cmd := ingestCmd()
	cmd.SetArgs([]string{
		"--dataset", "phs1", "--source", src, "--dest", dst,
		"--dua-id", "DUA-NOPE", "--allowed-dua", "DUA-1", "--allowed-source", dir + "/",
		"--steward-dir", filepath.Join(dir, ".steward"),
	})
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected a denial error")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Error("SECURITY: destination was written despite denial")
	}
}

func TestIngestCmd_RequiresCoreFlags(t *testing.T) {
	cmd := ingestCmd()
	cmd.SetArgs([]string{"--dataset", "phs1"}) // missing source/dest
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	if err := cmd.Execute(); err == nil {
		t.Error("expected an error when --source/--dest are missing")
	}
}
