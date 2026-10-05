// Package s3 is the object storage client (SeaweedFS locally; Cloudflare R2,
// AWS S3 or Backblaze B2 by configuration only).
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/msch128/mnema-talk/internal/config"
)

type Client struct {
	client *s3svc.Client
	bucket string
}

func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(cfg.S3Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load s3 config: %w", err)
	}
	c := &Client{
		client: s3svc.NewFromConfig(awsCfg, func(o *s3svc.Options) {
			if cfg.S3Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.S3Endpoint)
			}
			o.UsePathStyle = cfg.S3ForcePathStyle
		}),
		bucket: cfg.S3Bucket,
	}
	if err := c.ensureBucket(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

// bucketAPI is the part of the S3 client ensureBucket needs (tests fake it).
type bucketAPI interface {
	HeadBucket(ctx context.Context, in *s3svc.HeadBucketInput, opts ...func(*s3svc.Options)) (*s3svc.HeadBucketOutput, error)
	CreateBucket(ctx context.Context, in *s3svc.CreateBucketInput, opts ...func(*s3svc.Options)) (*s3svc.CreateBucketOutput, error)
}

func (c *Client) ensureBucket(ctx context.Context) error {
	return ensureBucket(ctx, c.client, c.bucket)
}

// ensureBucket creates the bucket only when HEAD says it does not exist. Any
// other HEAD failure (wrong credentials, storage down) is returned as is, so
// it is not mistaken for a missing bucket.
func ensureBucket(ctx context.Context, api bucketAPI, bucket string) error {
	_, err := api.HeadBucket(ctx, &s3svc.HeadBucketInput{Bucket: aws.String(bucket)})
	if err == nil {
		return nil
	}
	if !isNotFound(err) {
		return fmt.Errorf("check bucket %s: %w", bucket, err)
	}
	_, err = api.CreateBucket(ctx, &s3svc.CreateBucketInput{Bucket: aws.String(bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	switch {
	case err == nil:
		slog.Info("s3 bucket created", "bucket", bucket)
		return nil
	case errors.As(err, &owned):
		// Created concurrently by another instance.
		return nil
	case errors.As(err, &exists):
		// Either created concurrently, or owned by another account: only
		// a successful HEAD tells them apart.
		if _, herr := api.HeadBucket(ctx, &s3svc.HeadBucketInput{Bucket: aws.String(bucket)}); herr == nil {
			return nil
		}
		return fmt.Errorf("bucket %s exists but is not accessible with these credentials: %w", bucket, err)
	default:
		return fmt.Errorf("create bucket %s: %w", bucket, err)
	}
}

// isNotFound reports whether a HeadBucket error means the bucket is missing.
// HEAD responses carry no body, so besides the typed errors the SDK may only
// report the HTTP status.
func isNotFound(err error) bool {
	var nf *types.NotFound
	var nsb *types.NoSuchBucket
	if errors.As(err, &nf) || errors.As(err, &nsb) {
		return true
	}
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		switch coded.ErrorCode() {
		case "NotFound", "NoSuchBucket":
			return true
		}
	}
	var status interface{ HTTPStatusCode() int }
	return errors.As(err, &status) && status.HTTPStatusCode() == http.StatusNotFound
}

func (c *Client) Upload(ctx context.Context, key string, body io.Reader, mimeType string, size int64) error {
	_, err := c.client.PutObject(ctx, &s3svc.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(mimeType),
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("put object %s: %w", key, err)
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	if _, err := c.client.DeleteObject(ctx, &s3svc.DeleteObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)}); err != nil {
		return fmt.Errorf("delete object %s: %w", key, err)
	}
	return nil
}

// DeleteBatch deletes keys in chunks of 1000 (the S3 per-request maximum).
func (c *Client) DeleteBatch(ctx context.Context, keys []string) error {
	for start := 0; start < len(keys); start += 1000 {
		end := min(start+1000, len(keys))
		ids := make([]types.ObjectIdentifier, 0, end-start)
		for _, k := range keys[start:end] {
			ids = append(ids, types.ObjectIdentifier{Key: aws.String(k)})
		}
		out, err := c.client.DeleteObjects(ctx, &s3svc.DeleteObjectsInput{
			Bucket: aws.String(c.bucket),
			Delete: &types.Delete{Objects: ids, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return fmt.Errorf("delete objects: %w", err)
		}
		if len(out.Errors) > 0 {
			return fmt.Errorf("delete objects: %d of %d failed", len(out.Errors), len(ids))
		}
	}
	return nil
}

// GetObjectFrom streams the object from byte offset to its end.
func (c *Client) GetObjectFrom(ctx context.Context, key string, offset int64) (io.ReadCloser, error) {
	out, err := c.client.GetObject(ctx, &s3svc.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Range:  aws.String(fmt.Sprintf("bytes=%d-", offset)),
	})
	if err != nil {
		return nil, fmt.Errorf("get object %s from %d: %w", key, offset, err)
	}
	return out.Body, nil
}
