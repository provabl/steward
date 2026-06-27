// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package mover

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// CommandMover is the generic, exec-based transport: it runs an operator-configured
// command (e.g. `aws s3 cp`, `globus transfer`, `rclone copy`, a wrapper script) to
// move the bytes, then steward governs the result. This is steward's primary
// extensibility path — any transport an operator can invoke from a command line
// plugs in with **zero steward coupling**: steward needn't know Globus or DataSync
// exists, mirroring how vet shells out to cosign/syft/grype rather than importing
// their SDKs.
//
// Two safety properties make a fully generic mover sound:
//
//   - **No shell.** The command is run as argv (exec, not `sh -c`). The {source}
//     and {dest} placeholders are substituted as whole argv ELEMENTS, never
//     concatenated into a string a shell would re-split — so a crafted source/dest
//     cannot inject arguments or commands.
//   - **Zero trust in the transport.** steward does not believe the command about
//     what it moved. When the dest is a local path, this mover computes the sha256
//     itself after the copy; otherwise the digest is left empty and
//     `provenance verify` recomputes it against the dest before the gate passes.
//     A buggy or hostile mover cannot assert "intact" — steward checks.
type CommandMover struct {
	// argv is the command template. Exactly one element should be (or contain) the
	// {source} placeholder and one the {dest} placeholder; e.g.
	//   ["aws", "s3", "cp", "{source}", "{dest}"]
	//   ["globus", "transfer", "{source}", "{dest}"]
	argv   []string
	label  string // the Mechanism recorded in provenance (e.g. "s3cp", "globus")
	runner Runner
}

// Runner runs a command as argv (mockable in tests). It must NOT use a shell.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
}

type execRunner struct{}

func (execRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...) // #nosec G204 — operator-configured argv, no shell
	cmd.Stdout, cmd.Stderr = os.Stderr, os.Stderr
	return cmd.Run()
}

// NewCommandMover builds a CommandMover. label is the transport name recorded in
// provenance (e.g. "globus"); argv is the command template using {source}/{dest}.
// Returns an error if argv is empty or omits a placeholder — a template that can't
// reference both endpoints is a configuration mistake, caught early.
func NewCommandMover(label string, argv []string) (*CommandMover, error) {
	return newCommandMover(label, argv, execRunner{})
}

func newCommandMover(label string, argv []string, r Runner) (*CommandMover, error) {
	if len(argv) == 0 {
		return nil, fmt.Errorf("command mover needs a non-empty command template")
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "{source}") || !strings.Contains(joined, "{dest}") {
		return nil, fmt.Errorf("command template must reference both {source} and {dest}")
	}
	if label == "" {
		label = "command"
	}
	return &CommandMover{argv: argv, label: label, runner: r}, nil
}

// Name is the transport label recorded in provenance.
func (m *CommandMover) Name() string { return m.label }

// Move substitutes {source}/{dest} into the argv template (as whole elements),
// runs the command without a shell, and — for a local dest — computes the sha256
// of what landed. The digest is steward-observed, not command-reported; for a
// non-local dest it is left empty for `provenance verify` to recompute.
func (m *CommandMover) Move(ctx context.Context, req Request) (*Result, error) {
	args := make([]string, len(m.argv))
	for i, a := range m.argv {
		a = strings.ReplaceAll(a, "{source}", req.Source)
		a = strings.ReplaceAll(a, "{dest}", req.Dest)
		args[i] = a
	}
	if err := m.runner.Run(ctx, args[0], args[1:]...); err != nil {
		return nil, fmt.Errorf("mover command %q: %w", m.label, err)
	}

	res := &Result{Mechanism: m.label}
	// Zero-trust digest: if the dest is local, steward hashes it itself.
	if digest, n, ok := localDigest(req.Dest); ok {
		res.Digest = digest
		res.BytesMoved = n
	}
	return res, nil
}

// localDigest returns the sha256 + size of a local-path / file:// dest, or ok=false
// for a non-local dest (digest deferred to `provenance verify`).
func localDigest(dest string) (digest string, n int64, ok bool) {
	p := strings.TrimPrefix(dest, "file://")
	if strings.Contains(p, "://") {
		return "", 0, false // non-local scheme — verify recomputes against it later
	}
	f, err := os.Open(p) //nolint:gosec // operator-supplied destination they govern
	if err != nil {
		return "", 0, false
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err = io.Copy(h, f)
	if err != nil {
		return "", 0, false
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), n, true
}
