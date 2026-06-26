// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"fmt"
	"strings"
)

// PolicyAuthorizer is the v1, config-driven Authorizer: the operator supplies the
// DUAs the principal holds and the allowed source prefixes explicitly. It makes
// the authorize-before-move flow real and fully testable without AWS. The deferred
// IAM-tag authorizer (reading attest:nih-dua-ids off the principal, the same set
// the compute-to-data chain checks) implements the same Authorizer interface.
//
// Fail-closed by construction: an empty AllowedDUAs or AllowedSources denies
// everything for that dimension — you must explicitly grant.
type PolicyAuthorizer struct {
	// AllowedDUAs are the DUA ids the requesting principal is approved for. The
	// request's DUAID must be one of them.
	AllowedDUAs []string
	// AllowedSources are permitted source prefixes (e.g. "globus:dtn.ncbi.nlm.nih.gov").
	// The request's Source must start with one of them.
	AllowedSources []string
	// RequireDataClass, when set, requires the request's DataClass to match exactly
	// (the destination account's data-class posture, supplied by the operator).
	RequireDataClass string
}

// Authorize permits the ingestion only if the DUA is held, the source is allowed,
// and (when required) the data class matches. Each failed dimension yields a clear
// reason; the first failure short-circuits.
func (p PolicyAuthorizer) Authorize(_ context.Context, req Request) (Decision, error) {
	if req.DUAID == "" {
		return Decision{Reason: "no DUA id on the request (a current DUA is required to ingest)"}, nil
	}
	if !contains(p.AllowedDUAs, req.DUAID) {
		return Decision{Reason: fmt.Sprintf("principal is not approved for DUA %q", req.DUAID)}, nil
	}
	if !hasAllowedPrefix(req.Source, p.AllowedSources) {
		return Decision{Reason: fmt.Sprintf("source %q is not on the allowed list", req.Source)}, nil
	}
	if p.RequireDataClass != "" && req.DataClass != p.RequireDataClass {
		return Decision{Reason: fmt.Sprintf("data class %q does not match the required %q", req.DataClass, p.RequireDataClass)}, nil
	}
	return Decision{Permitted: true}, nil
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

func hasAllowedPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
