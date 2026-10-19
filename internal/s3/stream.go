package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3svc "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// partSize is S3's minimum size for every part but the last. A streamed
// upload holds exactly one part in memory, however large the file.
const partSize = 5 << 20

var partBuffers = sync.Pool{New: func() any { b := make([]byte, partSize); return &b }}

// objectAPI is the part of the S3 client a streamed upload needs (tests
// fake it).
type objectAPI interface {
	PutObject(ctx context.Context, in *s3svc.PutObjectInput, opts ...func(*s3svc.Options)) (*s3svc.PutObjectOutput, error)
	CreateMultipartUpload(ctx context.Context, in *s3svc.CreateMultipartUploadInput, opts ...func(*s3svc.Options)) (*s3svc.CreateMultipartUploadOutput, error)
	UploadPart(ctx context.Context, in *s3svc.UploadPartInput, opts ...func(*s3svc.Options)) (*s3svc.UploadPartOutput, error)
	CompleteMultipartUpload(ctx context.Context, in *s3svc.CompleteMultipartUploadInput, opts ...func(*s3svc.Options)) (*s3svc.CompleteMultipartUploadOutput, error)
	AbortMultipartUpload(ctx context.Context, in *s3svc.AbortMultipartUploadInput, opts ...func(*s3svc.Options)) (*s3svc.AbortMultipartUploadOutput, error)
}

// uploadStream stores body, whose length is unknown, under key. A body that
// fits in one part is a single PutObject; a larger one becomes a multipart
// upload, part by part, aborted again if reading or any request fails.
func uploadStream(ctx context.Context, api objectAPI, bucket, key string, body io.Reader, mimeType string) error {
	bufp := partBuffers.Get().(*[]byte)
	defer partBuffers.Put(bufp)
	buf := *bufp

	n, err := io.ReadFull(body, buf)
	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		_, err := api.PutObject(ctx, &s3svc.PutObjectInput{
			Bucket:        aws.String(bucket),
			Key:           aws.String(key),
			Body:          bytes.NewReader(buf[:n]),
			ContentType:   aws.String(mimeType),
			ContentLength: aws.Int64(int64(n)),
		})
		if err != nil {
			return fmt.Errorf("put object %s: %w", key, err)
		}
		return nil
	case err != nil:
		return err
	}

	mp := &multipartUpload{api: api, bucket: bucket, key: key}
	if err := mp.start(ctx, mimeType); err != nil {
		return err
	}
	return mp.run(ctx, body, buf, n)
}

// multipartUpload is one S3 multipart upload in progress.
type multipartUpload struct {
	api      objectAPI
	bucket   string
	key      string
	uploadID *string
}

func (m *multipartUpload) start(ctx context.Context, mimeType string) error {
	created, err := m.api.CreateMultipartUpload(ctx, &s3svc.CreateMultipartUploadInput{
		Bucket:      aws.String(m.bucket),
		Key:         aws.String(m.key),
		ContentType: aws.String(mimeType),
	})
	if err != nil {
		return fmt.Errorf("start multipart upload %s: %w", m.key, err)
	}
	m.uploadID = created.UploadId
	return nil
}

// abort drops the parts uploaded so far and returns cause.
func (m *multipartUpload) abort(ctx context.Context, cause error) error {
	// The request may already be cancelled; the parts must go anyway.
	if _, aerr := m.api.AbortMultipartUpload(context.WithoutCancel(ctx), &s3svc.AbortMultipartUploadInput{
		Bucket: aws.String(m.bucket), Key: aws.String(m.key), UploadId: m.uploadID,
	}); aerr != nil {
		slog.Warn("abort multipart upload", "key", m.key, "err", aerr)
	}
	return cause
}

// run uploads the first n bytes of buf and then the rest of body part by
// part, completes the upload and aborts it on any failure.
func (m *multipartUpload) run(ctx context.Context, body io.Reader, buf []byte, n int) error {
	var parts []types.CompletedPart
	for num := int32(1); ; num++ {
		out, err := m.api.UploadPart(ctx, &s3svc.UploadPartInput{
			Bucket:        aws.String(m.bucket),
			Key:           aws.String(m.key),
			UploadId:      m.uploadID,
			PartNumber:    aws.Int32(num),
			Body:          bytes.NewReader(buf[:n]),
			ContentLength: aws.Int64(int64(n)),
		})
		if err != nil {
			return m.abort(ctx, fmt.Errorf("upload part %d of %s: %w", num, m.key, err))
		}
		parts = append(parts, types.CompletedPart{ETag: out.ETag, PartNumber: aws.Int32(num)})
		if n < partSize {
			break // that was the short last part
		}
		n, err = io.ReadFull(body, buf)
		if errors.Is(err, io.EOF) {
			break // the body ended exactly on a part boundary
		}
		if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
			return m.abort(ctx, err)
		}
	}

	if _, err := m.api.CompleteMultipartUpload(ctx, &s3svc.CompleteMultipartUploadInput{
		Bucket:          aws.String(m.bucket),
		Key:             aws.String(m.key),
		UploadId:        m.uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		return m.abort(ctx, fmt.Errorf("complete multipart upload %s: %w", m.key, err))
	}
	return nil
}
