// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package ingest

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/iam"
)

// DUATagKey is the IAM role tag carrying the principal's approved DUA set — the
// same tag qualify writes on training/approval completion and the compute-to-data
// chain checks (provabl ADR 0002 / ADR 0003). It is a *delimited set*: a
// researcher can hold DUAs for several studies. steward reads it; it never writes
// it (qualify/attest own the attest:* namespace).
const DUATagKey = "attest:nih-dua-ids"

// roleTagLister reads an IAM role's tags. Satisfied by the IAM client in
// production; faked in tests.
type roleTagLister interface {
	ListRoleTags(ctx context.Context, in *iam.ListRoleTagsInput, optFns ...func(*iam.Options)) (*iam.ListRoleTagsOutput, error)
}

// IAMAuthorizer authorizes an ingestion against the principal's real IAM tags: it
// reads the `attest:nih-dua-ids` set off the requesting role and permits only when
// the request's DUA is in that set (and the source/data-class checks pass). This
// is the production Authorizer — it consumes the approval qualify recorded, rather
// than the operator-supplied allow-lists of the config-driven PolicyAuthorizer.
//
// steward is a *reader* here: it never writes attest:* tags. Fail-closed — if the
// role's tags can't be read, the ingestion does not proceed.
type IAMAuthorizer struct {
	iam roleTagLister
	// AllowedSources / RequireDataClass mirror PolicyAuthorizer: the DUA comes from
	// the principal's tag, but the allowed sources and the destination's data-class
	// posture are still operator/account config.
	AllowedSources   []string
	RequireDataClass string
}

// NewIAMAuthorizer builds an IAMAuthorizer backed by the AWS IAM client.
func NewIAMAuthorizer(ctx context.Context, region string, allowedSources []string, requireDataClass string) (*IAMAuthorizer, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &IAMAuthorizer{iam: iam.NewFromConfig(cfg), AllowedSources: allowedSources, RequireDataClass: requireDataClass}, nil
}

// Authorize permits the ingestion only if the requesting principal's
// attest:nih-dua-ids tag contains the request's DUA, the source is allowed, and
// (when required) the data class matches. Fail-closed: an unreadable role, or a
// principal ARN that isn't a role, is an error — not a denial — so the caller
// stops rather than silently proceeding.
func (a *IAMAuthorizer) Authorize(ctx context.Context, req Request) (Decision, error) {
	if req.Principal == "" {
		return Decision{Reason: "no principal on the request (the IAM authorizer needs the role ARN whose DUA tag it reads)"}, nil
	}
	if req.DUAID == "" {
		return Decision{Reason: "no DUA id on the request (a current DUA is required to ingest)"}, nil
	}
	roleName, err := roleNameFromARN(req.Principal)
	if err != nil {
		return Decision{}, fmt.Errorf("resolve principal %q: %w", req.Principal, err)
	}

	tags, err := a.roleTags(ctx, roleName)
	if err != nil {
		return Decision{}, fmt.Errorf("read tags for role %q (fail-closed): %w", roleName, err)
	}
	held := parseDUASet(tags[DUATagKey])
	if !held[req.DUAID] {
		return Decision{Reason: fmt.Sprintf("principal %s does not hold DUA %q in its %s tag", roleName, req.DUAID, DUATagKey)}, nil
	}
	if !hasAllowedPrefix(req.Source, a.AllowedSources) {
		return Decision{Reason: fmt.Sprintf("source %q is not on the allowed list", req.Source)}, nil
	}
	if a.RequireDataClass != "" && req.DataClass != a.RequireDataClass {
		return Decision{Reason: fmt.Sprintf("data class %q does not match the required %q", req.DataClass, a.RequireDataClass)}, nil
	}
	return Decision{Permitted: true}, nil
}

// roleTags reads all tags on the role (paginated) into a map.
func (a *IAMAuthorizer) roleTags(ctx context.Context, roleName string) (map[string]string, error) {
	out := map[string]string{}
	var marker *string
	for {
		resp, err := a.iam.ListRoleTags(ctx, &iam.ListRoleTagsInput{RoleName: aws.String(roleName), Marker: marker})
		if err != nil {
			return nil, err
		}
		for _, t := range resp.Tags {
			out[aws.ToString(t.Key)] = aws.ToString(t.Value)
		}
		if resp.IsTruncated {
			marker = resp.Marker
			continue
		}
		return out, nil
	}
}

// roleNameFromARN extracts the role name from an IAM role ARN
// (arn:aws:iam::<acct>:role/<name>, including a path like role/team/<name>).
func roleNameFromARN(arn string) (string, error) {
	i := strings.Index(arn, ":role/")
	if i == -1 {
		return "", fmt.Errorf("not an IAM role ARN (need arn:aws:iam::…:role/<name>)")
	}
	name := arn[i+len(":role/"):]
	// A role path yields "path/elements/<name>"; ListRoleTags wants the final name.
	if j := strings.LastIndex(name, "/"); j != -1 {
		name = name[j+1:]
	}
	if name == "" {
		return "", fmt.Errorf("empty role name in ARN")
	}
	return name, nil
}

// parseDUASet parses the delimited attest:nih-dua-ids tag value into a set. The
// tag is a *delimited set* (ADR 0002); accept comma / space / semicolon so steward
// is tolerant of the exact delimiter qualify/attest settle on, and trim each id.
func parseDUASet(v string) map[string]bool {
	set := map[string]bool{}
	for _, f := range strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	}) {
		if f = strings.TrimSpace(f); f != "" {
			set[f] = true
		}
	}
	return set
}
