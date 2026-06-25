// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package provenance

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// fakeReader serves fixed bytes (or an error) for any dest.
type fakeReader struct {
	data []byte
	err  error
}

func (f fakeReader) Open(context.Context, string) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(strings.NewReader(string(f.data))), nil
}

func digestOf(b []byte) string {
	h := sha256.Sum256(b)
	return fmt.Sprintf("sha256:%x", h[:])
}

func TestVerify_Matches(t *testing.T) {
	data := []byte("genomic data bytes")
	res, err := Verify(context.Background(), fakeReader{data: data}, "s3://sre/x/", digestOf(data))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Matched {
		t.Errorf("expected match; computed=%s recorded=%s", res.ComputedDigest, res.RecordedDigest)
	}
}

func TestVerify_Mismatch(t *testing.T) {
	res, err := Verify(context.Background(), fakeReader{data: []byte("actual bytes")}, "s3://sre/x/", "sha256:deadbeef")
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Matched {
		t.Error("expected mismatch for a wrong recorded digest")
	}
}

func TestVerify_CaseInsensitiveHex(t *testing.T) {
	data := []byte("abc")
	upper := strings.ToUpper(digestOf(data)) // SHA256:<HEX>
	res, _ := Verify(context.Background(), fakeReader{data: data}, "s3://sre/x/", upper)
	if !res.Matched {
		t.Error("hex comparison should be case-insensitive")
	}
}

func TestVerify_EmptyRecordedDigest(t *testing.T) {
	if _, err := Verify(context.Background(), fakeReader{data: []byte("x")}, "s3://sre/x/", ""); err == nil {
		t.Error("empty recorded digest: want error")
	}
}

func TestVerify_OpenError(t *testing.T) {
	if _, err := Verify(context.Background(), fakeReader{err: errors.New("no such object")}, "s3://sre/x/", "sha256:ab"); err == nil {
		t.Error("open error should propagate")
	}
}
