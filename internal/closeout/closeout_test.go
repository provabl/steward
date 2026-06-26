// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package closeout

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/provabl/steward/internal/store"
)

// fakeDestroyer scripts the State and records whether Destroy was called.
type fakeDestroyer struct {
	state      State
	stateErr   error
	destroyN   int
	destroyErr error
	destroyed  bool
}

func (f *fakeDestroyer) Name() string { return "fake" }
func (f *fakeDestroyer) State(context.Context, string) (State, error) {
	return f.state, f.stateErr
}
func (f *fakeDestroyer) Destroy(context.Context, string) (int, error) {
	f.destroyed = true
	if f.destroyErr != nil {
		return 0, f.destroyErr
	}
	return f.destroyN, nil
}

const dest = "s3://sre/genomic/phs000178/"

// seedRecord writes a provenance record for dest so gate 1 passes.
func seedRecord(t *testing.T, dua string) *store.Store {
	t.Helper()
	s := store.New(t.TempDir())
	if err := s.SaveRecord(&store.ProvenanceRecord{
		Dataset: "phs000178", Dest: dest, DUAID: dua, DataClass: "GENOMIC",
		Digest: "sha256:abc", RecordedAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	return s
}

func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

// Default (no Confirm) is a DRY RUN: gates checked, nothing destroyed.
func TestCloseout_DryRunDestroysNothing(t *testing.T) {
	s := seedRecord(t, "DUA-1")
	d := &fakeDestroyer{state: State{Objects: 3}}
	out, err := New(d, s).Closeout(context.Background(), dest, Request{DUAID: "DUA-1"})
	if err != nil {
		t.Fatalf("Closeout: %v", err)
	}
	if !out.DryRun || out.Objects != 3 {
		t.Errorf("expected a dry run reporting 3 objects, got %+v", out)
	}
	if d.destroyed {
		t.Error("SECURITY: a dry run must not destroy anything")
	}
	if out.Certificate != nil {
		t.Error("no certificate on a dry run")
	}
}

// Confirmed + retention elapsed → destroys + writes a certificate.
func TestCloseout_ConfirmedDestroysAndCertifies(t *testing.T) {
	s := seedRecord(t, "DUA-1")
	past := time.Now().Add(-time.Hour)
	d := &fakeDestroyer{state: State{Objects: 2, EarliestRetainUntil: &past}, destroyN: 2}
	out, err := New(d, s).Closeout(context.Background(), dest, Request{
		DUAID: "DUA-1", Confirm: true, Principal: "arn:role/Compliance",
	})
	if err != nil {
		t.Fatalf("Closeout: %v", err)
	}
	if !d.destroyed || out.Objects != 2 {
		t.Errorf("expected destruction of 2 objects, got %+v (destroyed=%v)", out, d.destroyed)
	}
	if out.Certificate == nil {
		t.Fatal("expected a destruction certificate")
	}
	cert := out.Certificate
	if cert.Dataset != "phs000178" || cert.DUAID != "DUA-1" || cert.DestroyedBy != "arn:role/Compliance" {
		t.Errorf("certificate fields wrong: %+v", cert)
	}
	if cert.Digest != "sha256:abc" {
		t.Error("certificate should carry the destroyed data's recorded digest")
	}
	// Persisted to the store.
	if got, _ := s.LoadCertificate(dest); got == nil {
		t.Error("certificate not persisted")
	}
}

// The load-bearing safety gate: retention NOT elapsed → refuse, destroy nothing.
func TestCloseout_RetentionNotElapsedRefuses(t *testing.T) {
	s := seedRecord(t, "DUA-1")
	future := time.Now().Add(90 * 24 * time.Hour)
	d := &fakeDestroyer{state: State{Objects: 2, EarliestRetainUntil: &future}, destroyN: 2}
	_, err := New(d, s).Closeout(context.Background(), dest, Request{
		DUAID: "DUA-1", Confirm: true, Principal: "arn:role/C",
	})
	if err == nil {
		t.Fatal("SECURITY: destruction before the retention term elapsed must be refused")
	}
	if d.destroyed {
		t.Error("SECURITY: nothing must be destroyed while retention is active")
	}
}

// Confirm without a principal → refuse (no anonymous destruction).
func TestCloseout_ConfirmWithoutPrincipalRefuses(t *testing.T) {
	s := seedRecord(t, "DUA-1")
	d := &fakeDestroyer{state: State{Objects: 1}}
	_, err := New(d, s).Closeout(context.Background(), dest, Request{DUAID: "DUA-1", Confirm: true})
	if err == nil {
		t.Error("a confirmed destruction must name the principal")
	}
	if d.destroyed {
		t.Error("must not destroy without a confirming principal")
	}
}

// No provenance record → refuse (only close out governed data).
func TestCloseout_NoRecordRefuses(t *testing.T) {
	s := store.New(t.TempDir()) // empty
	d := &fakeDestroyer{state: State{Objects: 1}}
	if _, err := New(d, s).Closeout(context.Background(), dest, Request{Confirm: true, Principal: "p"}); err == nil {
		t.Error("expected refusal when there is no provenance record")
	}
	if d.destroyed {
		t.Error("must not destroy data steward does not govern")
	}
}

// DUA mismatch → refuse.
func TestCloseout_DUAMismatchRefuses(t *testing.T) {
	s := seedRecord(t, "DUA-1")
	d := &fakeDestroyer{state: State{Objects: 1}}
	if _, err := New(d, s).Closeout(context.Background(), dest, Request{DUAID: "DUA-OTHER", Confirm: true, Principal: "p"}); err == nil {
		t.Error("expected refusal on DUA mismatch")
	}
	if d.destroyed {
		t.Error("must not destroy when the DUA does not match the record")
	}
}

// Unreadable destruction state → fail closed (no destroy).
func TestCloseout_StateErrorFailsClosed(t *testing.T) {
	s := seedRecord(t, "DUA-1")
	d := &fakeDestroyer{stateErr: errors.New("access denied")}
	if _, err := New(d, s).Closeout(context.Background(), dest, Request{DUAID: "DUA-1", Confirm: true, Principal: "p"}); err == nil {
		t.Error("expected fail-closed when the state can't be read")
	}
	if d.destroyed {
		t.Error("must not destroy when retention state is unknown")
	}
}

// Retention exactly at now (boundary): elapsed → allowed.
func TestCloseout_RetentionAtBoundaryAllowed(t *testing.T) {
	s := seedRecord(t, "DUA-1")
	now := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	ru := now.Add(-time.Second) // just elapsed
	d := &fakeDestroyer{state: State{Objects: 1, EarliestRetainUntil: &ru}, destroyN: 1}
	out, err := New(d, s).WithClock(fixedClock(now)).Closeout(context.Background(), dest, Request{
		DUAID: "DUA-1", Confirm: true, Principal: "p",
	})
	if err != nil || out.Certificate == nil {
		t.Fatalf("just-elapsed retention should allow destruction: %v", err)
	}
}
