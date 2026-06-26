// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package mover

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LocalMover is the reference Mover: it copies a local file (or file:// URI) to a
// local destination, computing the sha256 as it copies. It is the move-to-compute
// analogue of v1 `provenance verify`'s local-file handling — enough to exercise
// the full ingest flow (authorize → move → record) with no AWS, and the template
// the live movers (Globus / DataSync / s3cp) follow. The digest it reports is
// still ASSERTED in steward's model: `provenance verify` recomputes it against the
// destination before it counts as integrity-verified.
type LocalMover struct{}

// NewLocalMover returns the reference local-file mover.
func NewLocalMover() *LocalMover { return &LocalMover{} }

// Name is the transport label recorded in provenance.
func (*LocalMover) Name() string { return "local" }

// Move copies the source file to the destination, creating parent directories,
// and returns the bytes moved + the sha256 it observed during the copy. Source
// and Dest may be plain paths or file:// URIs; any other scheme is rejected (a
// real mover for that scheme is the deferred work).
func (*LocalMover) Move(_ context.Context, req Request) (*Result, error) {
	srcPath, err := localPath(req.Source)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	dstPath, err := localPath(req.Dest)
	if err != nil {
		return nil, fmt.Errorf("dest: %w", err)
	}

	in, err := os.Open(srcPath) //nolint:gosec // operator-supplied source they govern
	if err != nil {
		return nil, fmt.Errorf("open source %s: %w", srcPath, err)
	}
	defer func() { _ = in.Close() }()

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o750); err != nil {
		return nil, fmt.Errorf("create dest dir: %w", err)
	}
	out, err := os.Create(dstPath) //nolint:gosec // operator-supplied destination
	if err != nil {
		return nil, fmt.Errorf("create dest %s: %w", dstPath, err)
	}
	defer func() { _ = out.Close() }()

	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), in)
	if err != nil {
		return nil, fmt.Errorf("copy: %w", err)
	}
	if err := out.Close(); err != nil {
		return nil, fmt.Errorf("close dest: %w", err)
	}
	return &Result{
		Digest:     fmt.Sprintf("sha256:%x", h.Sum(nil)),
		BytesMoved: n,
		Mechanism:  "local",
	}, nil
}

// localPath turns a plain path or file:// URI into a filesystem path, rejecting
// any other scheme (those need a scheme-specific mover, which is deferred).
func localPath(ref string) (string, error) {
	p := strings.TrimPrefix(ref, "file://")
	if strings.Contains(p, "://") {
		return "", fmt.Errorf("the local mover handles local paths / file:// only; %q needs a scheme-specific mover (deferred)", ref)
	}
	return p, nil
}
