// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package closeout

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// s3API is the subset of the S3 client the destroyer needs (mockable).
type s3API interface {
	ListObjectsV2(ctx context.Context, in *s3.ListObjectsV2Input, optFns ...func(*s3.Options)) (*s3.ListObjectsV2Output, error)
	GetObjectRetention(ctx context.Context, in *s3.GetObjectRetentionInput, optFns ...func(*s3.Options)) (*s3.GetObjectRetentionOutput, error)
	ListObjectVersions(ctx context.Context, in *s3.ListObjectVersionsInput, optFns ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error)
	DeleteObject(ctx context.Context, in *s3.DeleteObjectInput, optFns ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

// s3Destroyer is the live Destroyer: it reports the prefix's object count +
// earliest Object Lock retention (so the Closer's elapsed-retention gate compares
// against the least-locked object), and on Destroy deletes every object *version*
// and delete-marker under the prefix (the bucket is versioned because it has
// Object Lock). The Closer enforces the safety gates before Destroy is ever called.
type s3Destroyer struct {
	client s3API
}

// NewS3Destroyer builds an S3-backed Destroyer for the region.
func NewS3Destroyer(ctx context.Context, region string) (Destroyer, error) {
	cfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(region))
	if err != nil {
		return nil, fmt.Errorf("load AWS config: %w", err)
	}
	return &s3Destroyer{client: s3.NewFromConfig(cfg)}, nil
}

// Name identifies the destroyer.
func (*s3Destroyer) Name() string { return "s3" }

func parseS3(dest string) (bucket, prefix string, err error) {
	if !strings.HasPrefix(dest, "s3://") {
		return "", "", fmt.Errorf("the s3 destroyer needs an s3://bucket/prefix dest, got %q", dest)
	}
	b, p, _ := strings.Cut(strings.TrimPrefix(dest, "s3://"), "/")
	if b == "" {
		return "", "", fmt.Errorf("no bucket in %q", dest)
	}
	return b, p, nil
}

// State reports the live object count and the earliest retention across the prefix.
func (d *s3Destroyer) State(ctx context.Context, dest string) (State, error) {
	bucket, prefix, err := parseS3(dest)
	if err != nil {
		return State{}, err
	}
	var st State
	var token *string
	for {
		out, err := d.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: &bucket, Prefix: aws.String(prefix), ContinuationToken: token,
		})
		if err != nil {
			return State{}, fmt.Errorf("list s3://%s/%s: %w", bucket, prefix, err)
		}
		for _, o := range out.Contents {
			st.Objects++
			ret, err := d.client.GetObjectRetention(ctx, &s3.GetObjectRetentionInput{Bucket: &bucket, Key: o.Key})
			if err == nil && ret.Retention != nil && ret.Retention.RetainUntilDate != nil {
				ru := *ret.Retention.RetainUntilDate
				if st.EarliestRetainUntil == nil || ru.Before(*st.EarliestRetainUntil) {
					st.EarliestRetainUntil = &ru
				}
			}
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		token = out.NextContinuationToken
	}
	return st, nil
}

// Destroy deletes every object version and delete-marker under the prefix. It is
// called only after the Closer's gates pass (governed data, retention elapsed,
// explicit confirmation). Returns the number of versions+markers removed.
func (d *s3Destroyer) Destroy(ctx context.Context, dest string) (int, error) {
	bucket, prefix, err := parseS3(dest)
	if err != nil {
		return 0, err
	}
	var removed int
	var keyMarker, versionMarker *string
	for {
		out, err := d.client.ListObjectVersions(ctx, &s3.ListObjectVersionsInput{
			Bucket: &bucket, Prefix: aws.String(prefix),
			KeyMarker: keyMarker, VersionIdMarker: versionMarker,
		})
		if err != nil {
			return removed, fmt.Errorf("list versions s3://%s/%s: %w", bucket, prefix, err)
		}
		for _, v := range out.Versions {
			if err := d.del(ctx, bucket, v.Key, v.VersionId); err != nil {
				return removed, err
			}
			removed++
		}
		for _, m := range out.DeleteMarkers {
			if err := d.del(ctx, bucket, m.Key, m.VersionId); err != nil {
				return removed, err
			}
			removed++
		}
		if out.IsTruncated == nil || !*out.IsTruncated {
			break
		}
		keyMarker, versionMarker = out.NextKeyMarker, out.NextVersionIdMarker
	}
	return removed, nil
}

func (d *s3Destroyer) del(ctx context.Context, bucket string, key, versionID *string) error {
	_, err := d.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: &bucket, Key: key, VersionId: versionID,
	})
	if err != nil {
		return fmt.Errorf("delete %s (version %s): %w", aws.ToString(key), aws.ToString(versionID), err)
	}
	return nil
}
