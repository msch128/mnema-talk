package s3

import (
	"context"
	"errors"
	"testing"

	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

type fakeBuckets struct {
	head    []error // returned by successive HeadBucket calls (last one repeats)
	create  error
	created int
}

func (f *fakeBuckets) HeadBucket(context.Context, *s3svc.HeadBucketInput, ...func(*s3svc.Options)) (*s3svc.HeadBucketOutput, error) {
	err := f.head[0]
	if len(f.head) > 1 {
		f.head = f.head[1:]
	}
	return &s3svc.HeadBucketOutput{}, err
}

func (f *fakeBuckets) CreateBucket(context.Context, *s3svc.CreateBucketInput, ...func(*s3svc.Options)) (*s3svc.CreateBucketOutput, error) {
	f.created++
	return &s3svc.CreateBucketOutput{}, f.create
}

type statusErr int

func (s statusErr) Error() string       { return "http error" }
func (s statusErr) HTTPStatusCode() int { return int(s) }

func TestEnsureBucket(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name       string
		f          *fakeBuckets
		wantErr    bool
		wantCreate int
	}{
		{"exists", &fakeBuckets{head: []error{nil}}, false, 0},
		{"missing typed", &fakeBuckets{head: []error{&types.NotFound{}}}, false, 1},
		{"missing by status", &fakeBuckets{head: []error{statusErr(404)}}, false, 1},
		{"forbidden is not missing", &fakeBuckets{head: []error{statusErr(403)}}, true, 0},
		{"storage down is not missing", &fakeBuckets{head: []error{errors.New("connection refused")}}, true, 0},
		{"owned by us after race", &fakeBuckets{head: []error{&types.NotFound{}}, create: &types.BucketAlreadyOwnedByYou{}}, false, 1},
		{"exists after race", &fakeBuckets{head: []error{&types.NotFound{}, nil}, create: &types.BucketAlreadyExists{}}, false, 1},
		{"exists, foreign", &fakeBuckets{head: []error{&types.NotFound{}, statusErr(403)}, create: &types.BucketAlreadyExists{}}, true, 1},
		{"create fails", &fakeBuckets{head: []error{&types.NotFound{}}, create: errors.New("boom")}, true, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ensureBucket(ctx, c.f, "b")
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, c.wantErr)
			}
			if c.f.created != c.wantCreate {
				t.Fatalf("created %d times, want %d", c.f.created, c.wantCreate)
			}
		})
	}
}
