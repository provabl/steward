// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/iam"
	iamtypes "github.com/aws/aws-sdk-go-v2/service/iam/types"
)

// fakeIAM serves a fixed tag map (or an error) for ListRoleTags.
type fakeIAM struct {
	tags    map[string]string
	err     error
	gotRole string
}

func (f *fakeIAM) ListRoleTags(_ context.Context, in *iam.ListRoleTagsInput, _ ...func(*iam.Options)) (*iam.ListRoleTagsOutput, error) {
	f.gotRole = aws.ToString(in.RoleName)
	if f.err != nil {
		return nil, f.err
	}
	var tags []iamtypes.Tag
	for k, v := range f.tags {
		tags = append(tags, iamtypes.Tag{Key: aws.String(k), Value: aws.String(v)})
	}
	return &iam.ListRoleTagsOutput{Tags: tags, IsTruncated: false}, nil
}

func iamAuth(f *fakeIAM, sources []string, class string) *IAMAuthorizer {
	return &IAMAuthorizer{iam: f, AllowedSources: sources, RequireDataClass: class}
}

func areq(dua, src, class string) Request {
	return Request{
		Dataset: "phs1", Source: src, Dest: "s3://x/", DUAID: dua, DataClass: class,
		Principal: "arn:aws:iam::123456789012:role/AliceResearchRole",
	}
}

func TestIAMAuthorize_HeldDUAPermits(t *testing.T) {
	f := &fakeIAM{tags: map[string]string{DUATagKey: "DUA-1,DUA-2,DUA-3"}}
	dec, err := iamAuth(f, []string{"globus:dtn"}, "GENOMIC").Authorize(context.Background(), areq("DUA-2", "globus:dtn/x", "GENOMIC"))
	if err != nil {
		t.Fatalf("Authorize: %v", err)
	}
	if !dec.Permitted {
		t.Errorf("DUA-2 is in the tag set; should permit (reason: %s)", dec.Reason)
	}
	if f.gotRole != "AliceResearchRole" {
		t.Errorf("read tags for role %q, want AliceResearchRole", f.gotRole)
	}
}

func TestIAMAuthorize_DUANotHeldDenies(t *testing.T) {
	f := &fakeIAM{tags: map[string]string{DUATagKey: "DUA-1 DUA-2"}}
	dec, _ := iamAuth(f, []string{"globus:dtn"}, "").Authorize(context.Background(), areq("DUA-9", "globus:dtn/x", ""))
	if dec.Permitted {
		t.Error("DUA-9 not in the set; must deny")
	}
	if dec.Reason == "" {
		t.Error("denial must carry a reason")
	}
}

func TestIAMAuthorize_NoDUATagDenies(t *testing.T) {
	f := &fakeIAM{tags: map[string]string{"other": "x"}} // no attest:nih-dua-ids
	dec, _ := iamAuth(f, []string{"globus:dtn"}, "").Authorize(context.Background(), areq("DUA-1", "globus:dtn/x", ""))
	if dec.Permitted {
		t.Error("a principal with no DUA tag holds no DUAs; must deny")
	}
}

func TestIAMAuthorize_SourceAndClassStillChecked(t *testing.T) {
	f := &fakeIAM{tags: map[string]string{DUATagKey: "DUA-1"}}
	// DUA held, but source not allowed.
	if dec, _ := iamAuth(f, []string{"globus:dtn"}, "").Authorize(context.Background(), areq("DUA-1", "globus:evil/x", "")); dec.Permitted {
		t.Error("source not on the allow-list must deny even when the DUA is held")
	}
	// DUA held + source ok, but wrong data class.
	if dec, _ := iamAuth(f, []string{"globus:dtn"}, "GENOMIC").Authorize(context.Background(), areq("DUA-1", "globus:dtn/x", "PHI")); dec.Permitted {
		t.Error("data-class mismatch must deny")
	}
}

// An unreadable role fails closed (error, not a silent deny/permit).
func TestIAMAuthorize_TagReadErrorFailsClosed(t *testing.T) {
	f := &fakeIAM{err: errors.New("AccessDenied")}
	if _, err := iamAuth(f, nil, "").Authorize(context.Background(), areq("DUA-1", "globus:dtn/x", "")); err == nil {
		t.Error("expected fail-closed error when role tags can't be read")
	}
}

func TestIAMAuthorize_NonRolePrincipalErrors(t *testing.T) {
	f := &fakeIAM{tags: map[string]string{DUATagKey: "DUA-1"}}
	req := areq("DUA-1", "globus:dtn/x", "")
	req.Principal = "arn:aws:iam::123456789012:user/bob" // a user, not a role
	if _, err := iamAuth(f, []string{"globus:dtn"}, "").Authorize(context.Background(), req); err == nil {
		t.Error("a non-role principal ARN should error (the authorizer reads role tags)")
	}
}

func TestRoleNameFromARN(t *testing.T) {
	cases := map[string]string{
		"arn:aws:iam::1:role/Alice":          "Alice",
		"arn:aws:iam::1:role/team/sub/Alice": "Alice", // role path → final element
	}
	for arn, want := range cases {
		got, err := roleNameFromARN(arn)
		if err != nil || got != want {
			t.Errorf("roleNameFromARN(%q) = (%q,%v), want %q", arn, got, err, want)
		}
	}
	if _, err := roleNameFromARN("arn:aws:iam::1:user/bob"); err == nil {
		t.Error("a user ARN should error")
	}
}

func TestParseDUASet(t *testing.T) {
	// comma, space, semicolon all accepted; whitespace trimmed; empties dropped.
	got := parseDUASet(" DUA-1, DUA-2 ;DUA-3  DUA-4 ")
	for _, want := range []string{"DUA-1", "DUA-2", "DUA-3", "DUA-4"} {
		if !got[want] {
			t.Errorf("parseDUASet missing %q (got %v)", want, got)
		}
	}
	if len(got) != 4 {
		t.Errorf("parseDUASet size = %d, want 4 (%v)", len(got), got)
	}
	if len(parseDUASet("")) != 0 {
		t.Error("empty tag → empty set")
	}
}
