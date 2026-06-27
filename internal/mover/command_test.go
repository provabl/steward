// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package mover

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// recordingRunner captures the argv it was asked to run and can be made to fail.
type recordingRunner struct {
	name string
	args []string
	err  error
	// onRun optionally performs a side effect (e.g. write the dest file).
	onRun func(name string, args []string)
}

func (r *recordingRunner) Run(_ context.Context, name string, args ...string) error {
	r.name, r.args = name, args
	if r.onRun != nil {
		r.onRun(name, args)
	}
	return r.err
}

func TestNewCommandMover_Validation(t *testing.T) {
	if _, err := NewCommandMover("x", nil); err == nil {
		t.Error("empty argv must error")
	}
	if _, err := NewCommandMover("x", []string{"cp", "{source}"}); err == nil {
		t.Error("template missing {dest} must error")
	}
	if _, err := NewCommandMover("x", []string{"cp", "{dest}"}); err == nil {
		t.Error("template missing {source} must error")
	}
	if _, err := NewCommandMover("x", []string{"cp", "{source}", "{dest}"}); err != nil {
		t.Errorf("valid template should construct: %v", err)
	}
}

func TestCommandMover_SubstitutesAndRuns(t *testing.T) {
	r := &recordingRunner{}
	m, _ := newCommandMover("s3cp", []string{"aws", "s3", "cp", "{source}", "{dest}"}, r)
	_, err := m.Move(context.Background(), Request{Source: "s3://in/a", Dest: "s3://out/b"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if r.name != "aws" {
		t.Errorf("command = %q, want aws", r.name)
	}
	want := []string{"s3", "cp", "s3://in/a", "s3://out/b"}
	if fmt.Sprint(r.args) != fmt.Sprint(want) {
		t.Errorf("args = %v, want %v", r.args, want)
	}
}

// Injection safety: a source containing shell metacharacters / spaces is passed
// as a SINGLE argv element, never split or interpreted.
func TestCommandMover_NoArginjection(t *testing.T) {
	r := &recordingRunner{}
	m, _ := newCommandMover("x", []string{"mv", "{source}", "{dest}"}, r)
	evil := "a b; rm -rf / && echo $(whoami)"
	_, err := m.Move(context.Background(), Request{Source: evil, Dest: "/tmp/d"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	// The whole malicious string must be exactly one argument — not split on
	// spaces, not interpreted as multiple args.
	if len(r.args) != 2 {
		t.Fatalf("expected 2 args (source, dest), got %d: %v", len(r.args), r.args)
	}
	if r.args[0] != evil {
		t.Errorf("source argument was altered/split: %q", r.args[0])
	}
}

// Zero-trust digest: for a local dest, the mover hashes what landed itself —
// independent of anything the command reported.
func TestCommandMover_SelfComputesLocalDigest(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "out.bin")
	data := []byte("moved bytes")
	r := &recordingRunner{onRun: func(_ string, _ []string) {
		_ = os.WriteFile(dst, data, 0o600) // simulate the command producing the file
	}}
	m, _ := newCommandMover("fake", []string{"cp", "{source}", "{dest}"}, r)

	res, err := m.Move(context.Background(), Request{Source: "/in", Dest: dst})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	want := fmt.Sprintf("sha256:%x", sha256.Sum256(data))
	if res.Digest != want {
		t.Errorf("digest = %s, want %s (steward must hash the dest itself)", res.Digest, want)
	}
	if res.BytesMoved != int64(len(data)) || res.Mechanism != "fake" {
		t.Errorf("result = %+v", res)
	}
}

// Non-local dest: digest is left empty (provenance verify recomputes it later).
func TestCommandMover_RemoteDestDefersDigest(t *testing.T) {
	r := &recordingRunner{}
	m, _ := newCommandMover("s3cp", []string{"aws", "s3", "cp", "{source}", "{dest}"}, r)
	res, err := m.Move(context.Background(), Request{Source: "/in", Dest: "s3://bucket/key"})
	if err != nil {
		t.Fatalf("Move: %v", err)
	}
	if res.Digest != "" {
		t.Errorf("remote dest digest should be empty (deferred to verify), got %q", res.Digest)
	}
	if res.Mechanism != "s3cp" {
		t.Errorf("mechanism = %q", res.Mechanism)
	}
}

func TestCommandMover_CommandFailurePropagates(t *testing.T) {
	r := &recordingRunner{err: errors.New("exit status 1")}
	m, _ := newCommandMover("x", []string{"cp", "{source}", "{dest}"}, r)
	if _, err := m.Move(context.Background(), Request{Source: "/in", Dest: "/out"}); err == nil {
		t.Error("a failing command must propagate as an error (no record written upstream)")
	}
}
