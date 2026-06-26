// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/provabl/steward/internal/mover"
	"github.com/provabl/steward/internal/store"
)

// fakeAuthorizer returns a scripted decision/error and records whether it ran.
type fakeAuthorizer struct {
	dec Decision
	err error
	ran bool
}

func (f *fakeAuthorizer) Authorize(context.Context, Request) (Decision, error) {
	f.ran = true
	return f.dec, f.err
}

// fakeMover records whether it moved and returns a scripted result/error.
type fakeMover struct {
	res   *mover.Result
	err   error
	moved bool
}

func (f *fakeMover) Name() string { return "fake" }
func (f *fakeMover) Move(context.Context, mover.Request) (*mover.Result, error) {
	f.moved = true
	if f.err != nil {
		return nil, f.err
	}
	return f.res, nil
}

func goodReq() Request {
	return Request{
		Dataset: "phs000178", Source: "globus:dtn/dbgap", Dest: "s3://sre/x/",
		DUAID: "DUA-1", DataClass: "GENOMIC", Principal: "arn:aws:iam::1:role/Alice",
	}
}

func newIngester(t *testing.T, a Authorizer, m mover.Mover) (*Ingester, *store.Store) {
	t.Helper()
	s := store.New(t.TempDir())
	ing := New(a, m, s).WithClock(func() time.Time { return time.Date(2026, 6, 26, 0, 0, 0, 0, time.UTC) })
	return ing, s
}

func TestIngest_HappyPath_AuthorizeMoveRecord(t *testing.T) {
	auth := &fakeAuthorizer{dec: Decision{Permitted: true}}
	mv := &fakeMover{res: &mover.Result{Digest: "sha256:abc", BytesMoved: 42, Mechanism: "fake"}}
	ing, s := newIngester(t, auth, mv)

	res, err := ing.Ingest(context.Background(), goodReq())
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if !auth.ran || !mv.moved {
		t.Errorf("expected authorize then move; ran=%v moved=%v", auth.ran, mv.moved)
	}
	if res.BytesMoved != 42 {
		t.Errorf("BytesMoved = %d", res.BytesMoved)
	}
	// A record was persisted, digest asserted (integrity_verified=false).
	rec, _ := s.LoadRecord("s3://sre/x/")
	if rec == nil {
		t.Fatal("expected a persisted provenance record")
	}
	if rec.Digest != "sha256:abc" || rec.IntegrityVerified {
		t.Errorf("record should carry the asserted digest unverified, got %+v", rec)
	}
	if rec.AuthorizedBy != "arn:aws:iam::1:role/Alice" || rec.Mover != "fake" {
		t.Errorf("record provenance fields wrong: %+v", rec)
	}
}

// Denied authorization must NOT move bytes and must NOT write a record.
func TestIngest_DeniedNeverMovesOrRecords(t *testing.T) {
	auth := &fakeAuthorizer{dec: Decision{Permitted: false, Reason: "no DUA"}}
	mv := &fakeMover{res: &mover.Result{Digest: "sha256:x"}}
	ing, s := newIngester(t, auth, mv)

	_, err := ing.Ingest(context.Background(), goodReq())
	if err == nil {
		t.Fatal("expected denial error")
	}
	if mv.moved {
		t.Error("SECURITY: bytes moved despite authorization denial")
	}
	if rec, _ := s.LoadRecord("s3://sre/x/"); rec != nil {
		t.Error("no record should be written on denial")
	}
}

// An authorizer that errors (can't evaluate) must fail closed — no move.
func TestIngest_AuthorizerErrorFailsClosed(t *testing.T) {
	auth := &fakeAuthorizer{err: errors.New("tag lookup failed")}
	mv := &fakeMover{res: &mover.Result{}}
	ing, _ := newIngester(t, auth, mv)
	if _, err := ing.Ingest(context.Background(), goodReq()); err == nil {
		t.Fatal("expected fail-closed error")
	}
	if mv.moved {
		t.Error("must not move when authorization can't be evaluated")
	}
}

// A mover error must NOT write a record (no provenance for a failed transfer).
func TestIngest_MoverErrorNoRecord(t *testing.T) {
	auth := &fakeAuthorizer{dec: Decision{Permitted: true}}
	mv := &fakeMover{err: errors.New("transfer aborted")}
	ing, s := newIngester(t, auth, mv)
	if _, err := ing.Ingest(context.Background(), goodReq()); err == nil {
		t.Fatal("expected mover error")
	}
	if rec, _ := s.LoadRecord("s3://sre/x/"); rec != nil {
		t.Error("no record should be written when the move fails")
	}
}

func TestIngest_RequiresCoreFields(t *testing.T) {
	ing, _ := newIngester(t, &fakeAuthorizer{dec: Decision{Permitted: true}}, &fakeMover{res: &mover.Result{}})
	if _, err := ing.Ingest(context.Background(), Request{Dataset: "x"}); err == nil {
		t.Error("expected an error when source/dest are missing")
	}
}
