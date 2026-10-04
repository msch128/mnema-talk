// Package s3 is the object storage client (SeaweedFS locally; Cloudflare R2,
// AWS S3 or Backblaze B2 by configuration only).
package s3

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

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

func (c *Client) ensureBucket(ctx context.Context) error {
	if _, err := c.client.HeadBucket(ctx, &s3svc.HeadBucketInput{Bucket: aws.String(c.bucket)}); err == nil {
		return nil
	}
	_, err := c.client.CreateBucket(ctx, &s3svc.CreateBucketInput{Bucket: aws.String(c.bucket)})
	var owned *types.BucketAlreadyOwnedByYou
	if err != nil && !errors.As(err, &owned) {
		return fmt.Errorf("create bucket %s: %w", c.bucket, err)
	}
	slog.Info("s3 bucket created", "bucket", c.bucket)
	return nil
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

func (c *Client) GetObject(ctx context.Context, key string) (io.ReadCloser, string, int64, error) {
	out, err := c.client.GetObject(ctx, &s3svc.GetObjectInput{Bucket: aws.String(c.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, "", 0, fmt.Errorf("get object %s: %w", key, err)
	}
	return out.Body, aws.ToString(out.ContentType), aws.ToInt64(out.ContentLength), nil
}
