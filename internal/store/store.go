// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package store manages the .steward/ directory and data-ingestion provenance
// records. It is the move-to-compute analogue of vet's .vet/ store: a
// ProvenanceRecord captures what data came in, from where, and under what
// authority; a GateResult is the lowered Cedar attribute attest reads as
// context.data.*.
package store

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const defaultStewardDir = ".steward"

// ProvenanceRecord is written to .steward/records/<key>.json when a dataset is
// ingested (or its prior out-of-band ingestion is recorded). It is the evidence
// steward's data:// appraiser judges: where the bytes came from, that they're
// intact, and the authority the ingestion ran under.
//
// IntegrityVerified means steward itself recomputed Digest against the bytes at
// Dest (via `steward provenance verify`) — NOT that a mover claimed it. A
// mover-asserted digest is recorded but stays IntegrityVerified=false until a
// verify re-check passes. This distinction is what makes the provenance claim
// mean "steward recomputed and matched," not "someone told us."
type ProvenanceRecord struct {
	Dataset           string    `json:"dataset"`                 // e.g. "phs000178"
	Dest              string    `json:"dest"`                    // where it landed, e.g. s3://bucket/genomic/phs000178/
	Source            string    `json:"source,omitempty"`        // where it came from, e.g. globus:dtn.ncbi.nlm.nih.gov/dbgap/phs000178
	Digest            string    `json:"digest,omitempty"`        // content digest (sha256:...) of the ingested data
	IntegrityVerified bool      `json:"integrity_verified"`      // true only when steward recomputed Digest against Dest
	DUAID             string    `json:"dua_id,omitempty"`        // governing Data Use Agreement
	DataClass         string    `json:"data_class,omitempty"`    // e.g. "GENOMIC" — matched against the destination's posture
	AuthorizedBy      string    `json:"authorized_by,omitempty"` // who authorized the ingestion
	Mover             string    `json:"mover,omitempty"`         // transport label, e.g. "globus" | "datasync" | "s3cp" | "out-of-band"
	RecordedAt        time.Time `json:"recorded_at"`
}

// GateResult is written to .steward/gate-result.json. Its fields are the Cedar
// attribute names attest's PDP reads from context.data.* — the data-plane
// counterpart to vet's context.workload.*.
type GateResult struct {
	Dataset            string    `json:"dataset"`
	Dest               string    `json:"dest"`
	ProvenanceVerified bool      `json:"provenance_verified"`
	SourceVerified     bool      `json:"source_verified"`
	IntegrityChecked   bool      `json:"integrity_checked"`
	DUAID              string    `json:"dua_id"`
	DataClass          string    `json:"data_class"`
	Digest             string    `json:"digest"`
	PolicyMet          bool      `json:"policy_met"`
	EvaluatedAt        time.Time `json:"evaluated_at"`
}

// Store manages the .steward/ directory.
type Store struct {
	dir string
}

// New creates a Store rooted at dir (defaults to .steward if empty).
func New(dir string) *Store {
	if dir == "" {
		dir = defaultStewardDir
	}
	return &Store{dir: dir}
}

// Default returns a Store at the default .steward path.
func Default() *Store { return New("") }

// Init creates the .steward/ directory structure.
func (s *Store) Init() error {
	if err := os.MkdirAll(filepath.Join(s.dir, "records"), 0o750); err != nil {
		return fmt.Errorf("create .steward/records: %w", err)
	}
	return nil
}

// SaveRecord writes a ProvenanceRecord to .steward/records/<key>.json. The key is
// derived from the destination, so re-recording the same destination overwrites
// (a fresh ingestion of the same prefix supersedes the prior record).
func (s *Store) SaveRecord(r *ProvenanceRecord) error {
	if err := s.Init(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal record: %w", err)
	}
	return os.WriteFile(filepath.Join(s.dir, "records", recordKey(r.Dest)+".json"), data, 0o640)
}

// LoadRecord reads .steward/records/<key>.json for the given destination.
// Returns nil, nil if not found (a fact about the world, not an error).
func (s *Store) LoadRecord(dest string) (*ProvenanceRecord, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, "records", recordKey(dest)+".json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read record: %w", err)
	}
	var r ProvenanceRecord
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("parse record: %w", err)
	}
	return &r, nil
}

// ListRecords returns every ProvenanceRecord in the store, for the audit log.
func (s *Store) ListRecords() ([]ProvenanceRecord, error) {
	entries, err := os.ReadDir(filepath.Join(s.dir, "records"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read records dir: %w", err)
	}
	var out []ProvenanceRecord
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, "records", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", e.Name(), err)
		}
		var r ProvenanceRecord
		if err := json.Unmarshal(data, &r); err != nil {
			return nil, fmt.Errorf("parse %s: %w", e.Name(), err)
		}
		out = append(out, r)
	}
	return out, nil
}

// SaveGateResult writes the gate result to .steward/gate-result.json.
func (s *Store) SaveGateResult(g *GateResult) error {
	if err := s.Init(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal gate result: %w", err)
	}
	return os.WriteFile(filepath.Join(s.dir, "gate-result.json"), data, 0o640)
}

// Dir returns the root .steward/ directory path.
func (s *Store) Dir() string { return s.dir }

// recordKey produces a filesystem-safe key from a destination reference (a short
// sha256 of the dest, matching vet's record-key scheme).
func recordKey(dest string) string {
	h := sha256.Sum256([]byte(dest))
	return fmt.Sprintf("%x", h[:8])
}
