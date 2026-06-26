// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package handling

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// DataClassTagKey is the object-tag key steward writes the data class under. It is
// a steward-owned tag on the *data* (not an attest:* principal tag) — the
// destination prefix's class marking that apply-handling makes provable.
const DataClassTagKey = "steward:data-class"

// s3API is the subset of the S3 client the backend needs (mockable).
type s3API interface {
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObjectTagging(ctx context.Context, in *s3.GetObjectTaggingInput, optFns ...func(*s3.Options)) (*s3.GetObjectTaggingOutput, error)
	PutObjectTagging(ctx context.Context, in *s3.PutObjectTaggingInput, optFns ...func(*s3.Options)) (*s3.PutObjectTaggingOutput, error)
	GetObjectRetention(ctx context.Context, in *s3.GetObjectRetentionInput, optFns ...func(*s3.Options)) (*s3.GetObjectRetentionOutput, error)
	PutObjectRetention(ctx context.Context, in *s3.PutObjectRetentionInput, optFns ...func(*s3.Options)) (*s3.PutObjectRetentionOutput, error)
}

// s3Backend is the live handling Backend: it tags every object under an
// s3://bucket/prefix destination with its data class and, when a retention is
// requested, sets S3 Object Lock retention (COMPLIANCE mode) on each. Object Lock
// is per-object, so the prefix is expanded to its objects. The Applier's no-relax
// invariant is enforced before Apply is called; this adapter only makes the calls.
type s3Backend struct {
	client s3API
}

// NewS3Backend builds an S3-backed handling Backend for the region.
func NewS3Backend(ctx context.Context, region string) (Backend, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &s3Backend{client: s3.NewFromConfig(cfg)}, nil
}

// Name identifies the backend.
func (*s3Backend) Name() string { return "s3" }

// parseS3 splits an s3://bucket/prefix destination.
func parseS3(dest string) (bucket, prefix string, err error) {
	if !strings.HasPrefix(dest, "s3://") {
		return "", "", fmt.Errorf("the s3 handling backend needs an s3://bucket/prefix dest, got %q", dest)
	}
	rest := strings.TrimPrefix(dest, "s3://")
	b, p, _ := strings.Cut(rest, "/")
	if b == "" {
		return "", "", fmt.Errorf("no bucket in %q", dest)
	}
	return b, p, nil
}

// Current reports the *weakest* handling across the prefix's objects: the earliest
// Object Lock retain-until found (so the Applier compares the new term against the
// least-protected object) and the data class of the first object carrying one.
// Returns the zero Current when the prefix has no objects or no handling yet.
func (b *s3Backend) Current(ctx context.Context, dest string) (Current, error) {
	bucket, prefix, err := parseS3(dest)
	if err != nil {
		return Current{}, err
	}
	keys, err := b.listKeys(ctx, bucket, prefix)
	if err != nil {
		return Current{}, err
	}
	var cur Current
	for _, k := range keys {
		// earliest retention across the prefix
		ret, err := b.client.GetObjectRetention(ctx, &s3.GetObjectRetentionInput{Bucket: &bucket, Key: aws.String(k)})
		if err == nil && ret.Retention != nil && ret.Retention.RetainUntilDate != nil {
			ru := *ret.Retention.RetainUntilDate
			if cur.RetainUntil == nil || ru.Before(*cur.RetainUntil) {
				cur.RetainUntil = &ru
			}
		}
		if cur.DataClass == "" {
			if dc := b.dataClassTag(ctx, bucket, k); dc != "" {
				cur.DataClass = dc
			}
		}
	}
	return cur, nil
}

// Apply tags every object under the prefix with the data class and, when
// spec.RetainUntil is set, applies COMPLIANCE-mode Object Lock retention to each.
func (b *s3Backend) Apply(ctx context.Context, spec Spec) error {
	bucket, prefix, err := parseS3(spec.Dest)
	if err != nil {
		return err
	}
	keys, err := b.listKeys(ctx, bucket, prefix)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("no objects under %s to apply handling to", spec.Dest)
	}
	for _, k := range keys {
		if _, err := b.client.PutObjectTagging(ctx, &s3.PutObjectTaggingInput{
			Bucket: &bucket, Key: aws.String(k),
			Tagging: &s3types.Tagging{TagSet: []s3types.Tag{
				{Key: aws.String(DataClassTagKey), Value: aws.String(spec.DataClass)},
			}},
		}); err != nil {
			return fmt.Errorf("tag %s: %w", k, err)
		}
		if spec.RetainUntil != nil {
			if _, err := b.client.PutObjectRetention(ctx, &s3.PutObjectRetentionInput{
				Bucket: &bucket, Key: aws.String(k),
				Retention: &s3types.ObjectLockRetention{
					Mode:            s3types.ObjectLockRetentionModeCompliance,
					RetainUntilDate: spec.RetainUntil,
				},
			}); err != nil {
				return fmt.Errorf("set retention on %s: %w", k, err)
			}
		}
	}
	return nil
}

// listKeys returns all object keys under bucket/prefix (paginated).
func (b *s3Backend) listKeys(ctx context.Context, bucket, prefix string) ([]string, error) {
	var keys []string
	var token *string
	for {
		out, err := b.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: &bucket, Prefix: aws.String(prefix), ContinuationToken: token,
		})
		if err != nil {
			return nil, fmt.Errorf("list s3://%s/%s: %w", bucket, prefix, err)
		}
		for _, o := range out.Contents {
			keys = append(keys, aws.ToString(o.Key))
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	return keys, nil
}

// dataClassTag returns the steward:data-class tag value on an object, or "".
func (b *s3Backend) dataClassTag(ctx context.Context, bucket, key string) string {
	out, err := b.client.GetObjectTagging(ctx, &s3.GetObjectTaggingInput{Bucket: &bucket, Key: aws.String(key)})
	if err != nil {
		return ""
	}
	for _, t := range out.TagSet {
		if aws.ToString(t.Key) == DataClassTagKey {
			return aws.ToString(t.Value)
		}
	}
	return ""
}
