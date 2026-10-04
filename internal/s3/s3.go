package s3

import (
	"context"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsConfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	s3Service "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/msch128/mnema-talk/internal/config"
)

type Client struct {
	client     *s3Service.Client
	bucketName string
}

func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
		if cfg.S3Endpoint != "" {
			return aws.Endpoint{
				PartitionID:       "aws",
				URL:               cfg.S3Endpoint,
				SigningRegion:     cfg.S3Region,
				HostnameImmutable: cfg.S3ForcePathStyle,
			}, nil
		}
		return aws.Endpoint{}, &aws.EndpointNotFoundError{}
	})

	loadedConfig, err := awsConfig.LoadDefaultConfig(ctx,
		awsConfig.WithRegion(cfg.S3Region),
		awsConfig.WithEndpointResolverWithOptions(customResolver),
		awsConfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.S3AccessKey, cfg.S3SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load S3 configuration: %w", err)
	}

	s3Cli := s3Service.NewFromConfig(loadedConfig, func(o *s3Service.Options) {
		o.UsePathStyle = cfg.S3ForcePathStyle
	})

	client := &Client{
		client:     s3Cli,
		bucketName: cfg.S3Bucket,
	}

	// Ensure target bucket exists
	if err := client.EnsureBucket(ctx); err != nil {
		log.Printf("[S3] Warning: could not verify bucket %s: %v\n", cfg.S3Bucket, err)
	}

	return client, nil
}

func (c *Client) EnsureBucket(ctx context.Context) error {
	_, err := c.client.HeadBucket(ctx, &s3Service.HeadBucketInput{
		Bucket: aws.String(c.bucketName),
	})
	if err != nil {
		log.Printf("[S3] Bucket '%s' does not exist, creating...\n", c.bucketName)
		_, createErr := c.client.CreateBucket(ctx, &s3Service.CreateBucketInput{
			Bucket: aws.String(c.bucketName),
		})
		if createErr != nil {
			return fmt.Errorf("failed to create S3 bucket %s: %w", c.bucketName, createErr)
		}
		log.Printf("[S3] Bucket '%s' created successfully\n", c.bucketName)
	}
	return nil
}

func (c *Client) Upload(ctx context.Context, key string, body io.Reader, mimeType string, size int64) error {
	_, err := c.client.PutObject(ctx, &s3Service.PutObjectInput{
		Bucket:        aws.String(c.bucketName),
		Key:           aws.String(key),
		Body:          body,
		ContentType:   aws.String(mimeType),
		ContentLength: aws.Int64(size),
	})
	if err != nil {
		return fmt.Errorf("failed to upload S3 object %s: %w", key, err)
	}
	return nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.client.DeleteObject(ctx, &s3Service.DeleteObjectInput{
		Bucket: aws.String(c.bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete S3 object %s: %w", key, err)
	}
	return nil
}

// DeleteBatch efficiently deletes multiple objects at once (crucial for 30-day pruning)
func (c *Client) DeleteBatch(ctx context.Context, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	var objectIds []types.ObjectIdentifier
	for _, k := range keys {
		objectIds = append(objectIds, types.ObjectIdentifier{
			Key: aws.String(k),
		})
	}

	_, err := c.client.DeleteObjects(ctx, &s3Service.DeleteObjectsInput{
		Bucket: aws.String(c.bucketName),
		Delete: &types.Delete{
			Objects: objectIds,
			Quiet:   aws.Bool(true),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to batch delete S3 objects: %w", err)
	}
	return nil
}

func (c *Client) GetObject(ctx context.Context, key string) (io.ReadCloser, string, int64, error) {
	out, err := c.client.GetObject(ctx, &s3Service.GetObjectInput{
		Bucket: aws.String(c.bucketName),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, "", 0, fmt.Errorf("failed to retrieve S3 object %s: %w", key, err)
	}

	mime := "application/octet-stream"
	if out.ContentType != nil {
		mime = *out.ContentType
	}
	size := int64(0)
	if out.ContentLength != nil {
		size = *out.ContentLength
	}

	return out.Body, mime, size, nil
}
