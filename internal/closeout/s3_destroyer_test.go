// SPDX-FileCopyrightText: 2026 Playground Logic LLC
// SPDX-License-Identifier: Apache-2.0

package closeout

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type fakeS3 struct {
	keys      []string
	retention map[string]time.Time
	versions  []s3types.ObjectVersion
	markers   []s3types.DeleteMarkerEntry
	deleted   []string // "key@version"
}

func (f *fakeS3) ListObjectsV2(_ context.Context, _ *s3.ListObjectsV2Input, _ ...func(*s3.Options)) (*s3.ListObjectsV2Output, error) {
	var c []s3types.Object
	for _, k := range f.keys {
		c = append(c, s3types.Object{Key: aws.String(k)})
	}
	return &s3.ListObjectsV2Output{Contents: c}, nil
}
func (f *fakeS3) GetObjectRetention(_ context.Context, in *s3.GetObjectRetentionInput, _ ...func(*s3.Options)) (*s3.GetObjectRetentionOutput, error) {
	out := &s3.GetObjectRetentionOutput{}
	if t, ok := f.retention[aws.ToString(in.Key)]; ok {
		out.Retention = &s3types.ObjectLockRetention{RetainUntilDate: aws.Time(t)}
	}
	return out, nil
}
func (f *fakeS3) ListObjectVersions(_ context.Context, _ *s3.ListObjectVersionsInput, _ ...func(*s3.Options)) (*s3.ListObjectVersionsOutput, error) {
	return &s3.ListObjectVersionsOutput{Versions: f.versions, DeleteMarkers: f.markers}, nil
}
func (f *fakeS3) DeleteObject(_ context.Context, in *s3.DeleteObjectInput, _ ...func(*s3.Options)) (*s3.DeleteObjectOutput, error) {
	f.deleted = append(f.deleted, aws.ToString(in.Key)+"@"+aws.ToString(in.VersionId))
	return &s3.DeleteObjectOutput{}, nil
}

func TestS3State_EarliestRetention(t *testing.T) {
	early := time.Now().Add(10 * 24 * time.Hour)
	late := time.Now().Add(300 * 24 * time.Hour)
	f := &fakeS3{
		keys:      []string{"g/a", "g/b"},
		retention: map[string]time.Time{"g/a": late, "g/b": early},
	}
	st, err := (&s3Destroyer{client: f}).State(context.Background(), "s3://bkt/g/")
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if st.Objects != 2 {
		t.Errorf("Objects = %d, want 2", st.Objects)
	}
	if st.EarliestRetainUntil == nil || !st.EarliestRetainUntil.Equal(early) {
		t.Errorf("EarliestRetainUntil = %v, want earliest %v", st.EarliestRetainUntil, early)
	}
}

func TestS3State_NoRetention(t *testing.T) {
	f := &fakeS3{keys: []string{"g/a"}}
	st, _ := (&s3Destroyer{client: f}).State(context.Background(), "s3://bkt/g/")
	if st.EarliestRetainUntil != nil {
		t.Error("no retention should report nil EarliestRetainUntil")
	}
}

// Destroy deletes every version AND delete-marker under the prefix.
func TestS3Destroy_DeletesVersionsAndMarkers(t *testing.T) {
	f := &fakeS3{
		versions: []s3types.ObjectVersion{
			{Key: aws.String("g/a"), VersionId: aws.String("v1")},
			{Key: aws.String("g/b"), VersionId: aws.String("v2")},
		},
		markers: []s3types.DeleteMarkerEntry{
			{Key: aws.String("g/a"), VersionId: aws.String("m1")},
		},
	}
	n, err := (&s3Destroyer{client: f}).Destroy(context.Background(), "s3://bkt/g/")
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if n != 3 {
		t.Errorf("removed = %d, want 3 (2 versions + 1 marker)", n)
	}
	want := map[string]bool{"g/a@v1": true, "g/b@v2": true, "g/a@m1": true}
	for _, d := range f.deleted {
		if !want[d] {
			t.Errorf("unexpected delete %q", d)
		}
		delete(want, d)
	}
	if len(want) != 0 {
		t.Errorf("not all versions/markers deleted; missing %v", want)
	}
}

func TestParseS3(t *testing.T) {
	b, p, err := parseS3("s3://bkt/g/phs1/")
	if err != nil || b != "bkt" || p != "g/phs1/" {
		t.Errorf("parseS3 = (%q,%q,%v)", b, p, err)
	}
	if _, _, err := parseS3("/local"); err == nil {
		t.Error("non-s3 dest should error")
	}
}
