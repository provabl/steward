// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

// Package audit queries the steward store for the ingestion audit trail: every
// dataset brought in, from where, under which DUA, and whether its provenance was
// verified — the record an auditor asks for.
package audit

import (
	"sort"

	"github.com/provabl/steward/internal/store"
)

// Filter narrows the ingestion log. Zero-value fields are not applied.
type Filter struct {
	DataClass string // exact match, e.g. "GENOMIC"
	DUAID     string // exact match
}

// Query returns the matching provenance records, newest first.
func Query(s *store.Store, f Filter) ([]store.ProvenanceRecord, error) {
	recs, err := s.ListRecords()
	if err != nil {
		return nil, err
	}
	out := recs[:0:0]
	for _, r := range recs {
		if f.DataClass != "" && r.DataClass != f.DataClass {
			continue
		}
		if f.DUAID != "" && r.DUAID != f.DUAID {
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RecordedAt.After(out[j].RecordedAt) })
	return out, nil
}
