// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package handling

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// fakeS3 serves a fixed object list + per-key retention/tags, and records writes.
type fakeS3 struct {
	keys        []string
	retention   map[string]time.Time // key → retain-until
	tags        map[string]string    // key → data-class tag value
	putTags     map[string]string    // recorded PutObjectTagging
	putRetained map[string]time.Time // recorded PutObjectRetention
}

func (f *fakeS3) ListObjectsV2(_ context.Context, in *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	var c []s3types.Object
	for _, k := range f.keys {
		c = append(c, s3types.Object{Key: aws.String(k)})
	}
	return &s3.ListObjectsV2Output{Contents: c}, nil
}
func (f *fakeS3) GetObjectTagging(_ context.Context, in *s3.GetObjectTaggingInput, _ ...func(*s3.Options)) (*s3.GetObjectTaggingOutput, error) {
	out := &s3.GetObjectTaggingOutput{}
	if v, ok := f.tags[aws.ToString(in.Key)]; ok {
		out.TagSet = []s3types.Tag{{Key: aws.String(DataClassTagKey), Value: aws.String(v)}}
	}
	return out, nil
}
func (f *fakeS3) PutObjectTagging(_ context.Context, in *s3.PutObjectTaggingInput, _ ...func(*s3.Options)) (*s3.PutObjectTaggingOutput, error) {
	if f.putTags == nil {
		f.putTags = map[string]string{}
	}
	for _, t := range in.Tagging.TagSet {
		if aws.ToString(t.Key) == DataClassTagKey {
			f.putTags[aws.ToString(in.Key)] = aws.ToString(t.Value)
		}
	}
	return &s3.PutObjectTaggingOutput{}, nil
}
func (f *fakeS3) GetObjectRetention(_ context.Context, in *s3.GetObjectRetentionInput, _ ...func(*s3.Options)) (*s3.GetObjectRetentionOutput, error) {
	out := &s3.GetObjectRetentionOutput{}
	if t, ok := f.retention[aws.ToString(in.Key)]; ok {
		out.Retention = &s3types.ObjectLockRetention{RetainUntilDate: aws.Time(t)}
	}
	return out, nil
}
func (f *fakeS3) PutObjectRetention(_ context.Context, in *s3.PutObjectRetentionInput, _ ...func(*s3.Options)) (*s3.PutObjectRetentionOutput, error) {
	if f.putRetained == nil {
		f.putRetained = map[string]time.Time{}
	}
	f.putRetained[aws.ToString(in.Key)] = *in.Retention.RetainUntilDate
	return &s3.PutObjectRetentionOutput{}, nil
}

func TestParseS3(t *testing.T) {
	b, p, err := parseS3("s3://my-bucket/genomic/phs1/")
	if err != nil || b != "my-bucket" || p != "genomic/phs1/" {
		t.Errorf("parseS3 = (%q,%q,%v)", b, p, err)
	}
	if _, _, err := parseS3("/local/path"); err == nil {
		t.Error("non-s3 dest should error")
	}
}

// Current reports the EARLIEST retention across the prefix (the weakest-protected
// object), so the Applier's no-relax check compares against the least-locked one.
func TestS3Current_ReportsEarliestRetention(t *testing.T) {
	early := time.Now().Add(30 * 24 * time.Hour)
	late := time.Now().Add(400 * 24 * time.Hour)
	f := &fakeS3{
		keys:      []string{"genomic/a", "genomic/b"},
		retention: map[string]time.Time{"genomic/a": late, "genomic/b": early},
		tags:      map[string]string{"genomic/a": "GENOMIC"},
	}
	cur, err := (&s3Backend{client: f}).Current(context.Background(), "s3://bkt/genomic/")
	if err != nil {
		t.Fatalf("Current: %v", err)
	}
	if cur.RetainUntil == nil || !cur.RetainUntil.Equal(early) {
		t.Errorf("RetainUntil = %v, want earliest %v", cur.RetainUntil, early)
	}
	if cur.DataClass != "GENOMIC" {
		t.Errorf("DataClass = %q", cur.DataClass)
	}
}

// Apply tags + sets retention on every object under the prefix.
func TestS3Apply_TagsAndRetainsEveryObject(t *testing.T) {
	until := time.Now().Add(365 * 24 * time.Hour)
	f := &fakeS3{keys: []string{"genomic/a", "genomic/b"}}
	err := (&s3Backend{client: f}).Apply(context.Background(), Spec{
		Dest: "s3://bkt/genomic/", DataClass: "GENOMIC", RetainUntil: &until,
	})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	for _, k := range []string{"genomic/a", "genomic/b"} {
		if f.putTags[k] != "GENOMIC" {
			t.Errorf("object %s not tagged GENOMIC (got %q)", k, f.putTags[k])
		}
		if !f.putRetained[k].Equal(until) {
			t.Errorf("object %s retention = %v, want %v", k, f.putRetained[k], until)
		}
	}
}

// No objects under the prefix → Apply errors (nothing to handle, fail-closed).
func TestS3Apply_EmptyPrefixErrors(t *testing.T) {
	f := &fakeS3{keys: nil}
	if err := (&s3Backend{client: f}).Apply(context.Background(), Spec{Dest: "s3://bkt/none/", DataClass: "G"}); err == nil {
		t.Error("expected an error when the prefix has no objects")
	}
}

// Without a retention in the spec, Apply tags but sets no retention.
func TestS3Apply_TagOnlyNoRetention(t *testing.T) {
	f := &fakeS3{keys: []string{"genomic/a"}}
	if err := (&s3Backend{client: f}).Apply(context.Background(), Spec{Dest: "s3://bkt/genomic/", DataClass: "G"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if f.putTags["genomic/a"] != "G" {
		t.Error("object should be tagged")
	}
	if len(f.putRetained) != 0 {
		t.Error("no retention should be set when spec.RetainUntil is nil")
	}
}
