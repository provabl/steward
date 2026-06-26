// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"testing"
)

func TestPolicyAuthorizer(t *testing.T) {
	base := PolicyAuthorizer{
		AllowedDUAs:      []string{"DUA-1", "DUA-2"},
		AllowedSources:   []string{"globus:dtn.ncbi.nlm.nih.gov"},
		RequireDataClass: "GENOMIC",
	}
	req := func(dua, src, class string) Request {
		return Request{Dataset: "phs1", Source: src, Dest: "s3://x/", DUAID: dua, DataClass: class}
	}
	cases := []struct {
		name      string
		req       Request
		permitted bool
	}{
		{"all good", req("DUA-1", "globus:dtn.ncbi.nlm.nih.gov/dbgap/phs1", "GENOMIC"), true},
		{"no DUA on request", req("", "globus:dtn.ncbi.nlm.nih.gov/x", "GENOMIC"), false},
		{"DUA not held", req("DUA-9", "globus:dtn.ncbi.nlm.nih.gov/x", "GENOMIC"), false},
		{"source not allowed", req("DUA-1", "globus:other.example.org/x", "GENOMIC"), false},
		{"wrong data class", req("DUA-1", "globus:dtn.ncbi.nlm.nih.gov/x", "PHI"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec, err := base.Authorize(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("Authorize: %v", err)
			}
			if dec.Permitted != tc.permitted {
				t.Errorf("permitted = %v, want %v (reason: %s)", dec.Permitted, tc.permitted, dec.Reason)
			}
			if !dec.Permitted && dec.Reason == "" {
				t.Error("a denial must carry a reason")
			}
		})
	}
}

// Empty allow-lists deny by construction (fail-closed — you must grant explicitly).
func TestPolicyAuthorizer_EmptyListsDeny(t *testing.T) {
	var p PolicyAuthorizer // zero value
	dec, _ := p.Authorize(context.Background(), Request{DUAID: "DUA-1", Source: "globus:x"})
	if dec.Permitted {
		t.Error("zero-value authorizer must deny (no DUA granted)")
	}
}
