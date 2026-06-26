// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package handling

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeBackend records what was applied and serves a scripted Current state.
type fakeBackend struct {
	current    Current
	currentErr error
	applyErr   error
	applied    *Spec
}

func (f *fakeBackend) Name() string { return "fake" }
func (f *fakeBackend) Current(context.Context, string) (Current, error) {
	return f.current, f.currentErr
}
func (f *fakeBackend) Apply(_ context.Context, spec Spec) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	s := spec
	f.applied = &s
	return nil
}

func ptr(t time.Time) *time.Time { return &t }

func TestApply_FreshDestination(t *testing.T) {
	until := time.Now().Add(365 * 24 * time.Hour)
	b := &fakeBackend{} // no current handling
	res, err := New(b).Apply(context.Background(), Spec{
		Dest: "s3://sre/x/", DataClass: "GENOMIC", DUAID: "DUA-1", RetainUntil: &until,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if b.applied == nil || b.applied.DataClass != "GENOMIC" {
		t.Errorf("backend not applied with the spec: %+v", b.applied)
	}
	if !res.RetentionSet || res.Backend != "fake" {
		t.Errorf("result wrong: %+v", res)
	}
}

func TestApply_RequiresDestAndClass(t *testing.T) {
	b := &fakeBackend{}
	if _, err := New(b).Apply(context.Background(), Spec{Dest: "s3://x/"}); err == nil {
		t.Error("expected an error when data-class is missing")
	}
	if b.applied != nil {
		t.Error("nothing should be applied on a validation failure")
	}
}

func TestApply_RetainUntilMustBeFuture(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	b := &fakeBackend{}
	if _, err := New(b).Apply(context.Background(), Spec{Dest: "s3://x/", DataClass: "G", RetainUntil: &past}); err == nil {
		t.Error("expected an error for a past retain-until")
	}
}

// The load-bearing invariant: extending retention is allowed.
func TestApply_ExtendRetentionAllowed(t *testing.T) {
	cur := time.Now().Add(30 * 24 * time.Hour)
	longer := time.Now().Add(400 * 24 * time.Hour)
	b := &fakeBackend{current: Current{DataClass: "GENOMIC", RetainUntil: &cur}}
	if _, err := New(b).Apply(context.Background(), Spec{Dest: "s3://x/", DataClass: "GENOMIC", RetainUntil: &longer}); err != nil {
		t.Fatalf("extending retention should be allowed: %v", err)
	}
	if b.applied == nil {
		t.Error("the extended retention should have been applied")
	}
}

// Shortening an existing retention must be refused — a DUA term cannot be weakened.
func TestApply_ShortenRetentionRefused(t *testing.T) {
	cur := time.Now().Add(400 * 24 * time.Hour)
	shorter := time.Now().Add(30 * 24 * time.Hour)
	b := &fakeBackend{current: Current{RetainUntil: &cur}}
	_, err := New(b).Apply(context.Background(), Spec{Dest: "s3://x/", DataClass: "G", RetainUntil: &shorter})
	if err == nil {
		t.Fatal("SECURITY: shortening an Object Lock retention must be refused")
	}
	if b.applied != nil {
		t.Error("nothing must be applied when the spec would relax handling")
	}
}

// Removing an existing retention (nil RetainUntil over an existing lock) is refused.
func TestApply_RemoveRetentionRefused(t *testing.T) {
	cur := time.Now().Add(100 * 24 * time.Hour)
	b := &fakeBackend{current: Current{RetainUntil: &cur}}
	if _, err := New(b).Apply(context.Background(), Spec{Dest: "s3://x/", DataClass: "G"}); err == nil {
		t.Error("removing an existing retention must be refused")
	}
}

// A backend that can't read current state fails closed — nothing applied.
func TestApply_CurrentErrorFailsClosed(t *testing.T) {
	b := &fakeBackend{currentErr: errors.New("access denied")}
	if _, err := New(b).Apply(context.Background(), Spec{Dest: "s3://x/", DataClass: "G"}); err == nil {
		t.Error("expected fail-closed when current handling can't be read")
	}
	if b.applied != nil {
		t.Error("nothing should be applied when the current state is unknown")
	}
}

func TestApply_BackendApplyError(t *testing.T) {
	b := &fakeBackend{applyErr: errors.New("put failed")}
	if _, err := New(b).Apply(context.Background(), Spec{Dest: "s3://x/", DataClass: "G", RetainUntil: ptr(time.Now().Add(time.Hour))}); err == nil {
		t.Error("expected the backend apply error to propagate")
	}
}
