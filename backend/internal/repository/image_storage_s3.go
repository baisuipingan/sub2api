package repository

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/servertiming"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
)

// S3ImageStorage 用 S3 兼容对象存储实现 service.ImageStorage。
type S3ImageStorage struct {
	client        *s3.Client
	bucket        string
	publicBaseURL string
	presignExpiry time.Duration
}

var _ service.ImageStorage = (*S3ImageStorage)(nil)

// NewS3ImageStorage 依据配置构造 S3 图片存储（调用方应先确认 cfg.Active()）。
func NewS3ImageStorage(ctx context.Context, cfg *config.ImageStorageConfig) (*S3ImageStorage, error) {
	client, err := newS3Client(ctx, s3ClientParams{
		Endpoint:        cfg.Endpoint,
		Region:          cfg.Region,
		AccessKeyID:     cfg.AccessKeyID,
		SecretAccessKey: cfg.SecretAccessKey,
		ForcePathStyle:  cfg.ForcePathStyle,
	})
	if err != nil {
		return nil, err
	}

	expiry := time.Duration(cfg.PresignExpiry) * time.Hour
	if expiry <= 0 {
		expiry = 24 * time.Hour
	}

	return &S3ImageStorage{
		client:        client,
		bucket:        cfg.Bucket,
		publicBaseURL: strings.TrimRight(cfg.PublicBaseURL, "/"),
		presignExpiry: expiry,
	}, nil
}

// Save 上传图片字节，返回可访问 URL：配了 public_base_url 则返回公开直链，否则返回 presigned 临时链接。
func (s *S3ImageStorage) Save(ctx context.Context, key, contentType string, data []byte) (string, error) {
	finish := servertiming.ObserveDependency(ctx, "s3")
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &key,
		Body:        bytes.NewReader(data),
		ContentType: &contentType,
	})
	finish()
	if err != nil {
		return "", fmt.Errorf("S3 PutObject: %w", err)
	}

	if s.publicBaseURL != "" {
		return s.publicBaseURL + "/" + strings.TrimLeft(key, "/"), nil
	}

	presignClient := s3.NewPresignClient(s.client)
	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(s.presignExpiry))
	if err != nil {
		return "", fmt.Errorf("presign url: %w", err)
	}
	return result.URL, nil
}

// ProbeReadWriteDelete checks the permissions the private Studio gallery needs.
// Constructing an S3 client alone does not contact the bucket.
func (s *S3ImageStorage) ProbeReadWriteDelete(ctx context.Context) (probeErr error) {
	return s.ProbeReadWriteDeletePrefix(ctx, "studio/v1/")
}

// ProbeReadWriteDeletePrefix also supports credentials scoped to ticket objects.
func (s *S3ImageStorage) ProbeReadWriteDeletePrefix(ctx context.Context, prefix string) (probeErr error) {
	key := strings.TrimRight(prefix, "/") + "/_connection-test/" + uuid.NewString()
	data := []byte("private-storage-probe")
	contentType := "text/plain"
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, Body: bytes.NewReader(data), ContentType: &contentType,
	}); err != nil {
		return fmt.Errorf("gallery storage probe put: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := s.client.DeleteObject(cleanup, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key}); err != nil && probeErr == nil {
			probeErr = fmt.Errorf("gallery storage probe delete: %w", err)
		}
	}()
	object, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return fmt.Errorf("gallery storage probe get: %w", err)
	}
	defer func() { _ = object.Body.Close() }()
	got, err := io.ReadAll(io.LimitReader(object.Body, int64(len(data)+1)))
	if err != nil {
		return fmt.Errorf("gallery storage probe read: %w", err)
	}
	if !bytes.Equal(got, data) {
		return fmt.Errorf("gallery storage probe returned unexpected content")
	}
	return nil
}

// ProbeSignedOfficeRead checks the endpoint used by external readers, in
// addition to the authenticated SDK calls performed by the connection probe.
func (s *S3ImageStorage) ProbeSignedOfficeRead(ctx context.Context, prefix string) (probeErr error) {
	key := strings.TrimRight(prefix, "/") + "/_connection-test/" + uuid.NewString()
	data := []byte("office-signed-read-probe")
	link, err := s3.NewPresignClient(s.client).PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: &s.bucket,
		Key:    &key,
	}, s3.WithPresignExpires(2*time.Minute))
	if err != nil {
		return err
	}
	contentType := "text/plain"
	if _, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, Body: bytes.NewReader(data), ContentType: &contentType,
	}); err != nil {
		return fmt.Errorf("office signed read probe put: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if _, err := s.client.DeleteObject(cleanup, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key}); err != nil && probeErr == nil {
			probeErr = fmt.Errorf("office signed read probe cleanup: %w", err)
		}
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, link.URL, nil)
	if err != nil {
		return fmt.Errorf("office signed read request could not be prepared")
	}
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("office signed read could not reach the storage endpoint")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("office signed read returned HTTP %d", response.StatusCode)
	}
	got, err := io.ReadAll(io.LimitReader(response.Body, int64(len(data)+1)))
	if err != nil || !bytes.Equal(got, data) {
		return fmt.Errorf("office signed read returned unexpected content")
	}
	return nil
}
