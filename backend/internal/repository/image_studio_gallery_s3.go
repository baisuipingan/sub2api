package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type s3ImageStudioGalleryStore struct {
	client *s3.Client
	bucket string
}

func NewImageStudioGalleryObjectStoreFactory() service.GalleryObjectStoreFactory {
	return func(ctx context.Context, cfg *config.ImageStorageConfig) (service.GalleryObjectStore, error) {
		client, err := newS3Client(ctx, s3ClientParams{
			Endpoint: cfg.Endpoint, Region: cfg.Region, AccessKeyID: cfg.AccessKeyID,
			SecretAccessKey: cfg.SecretAccessKey, ForcePathStyle: cfg.ForcePathStyle,
		})
		if err != nil {
			return nil, err
		}
		return &s3ImageStudioGalleryStore{client: client, bucket: cfg.Bucket}, nil
	}
}

func (s *s3ImageStudioGalleryStore) Put(ctx context.Context, key, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, Body: bytes.NewReader(data), ContentType: &contentType,
	})
	if err != nil {
		return fmt.Errorf("gallery PutObject: %w", err)
	}
	return nil
}

func (s *s3ImageStudioGalleryStore) Get(ctx context.Context, key string, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 || maxBytes > service.GalleryMaxImageBytes {
		return nil, service.ErrGalleryInvalid
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) && (api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "NotFound") {
			return nil, service.ErrGalleryNotFound
		}
		return nil, fmt.Errorf("gallery GetObject: %w", err)
	}
	defer func() { _ = result.Body.Close() }()
	if result.ContentLength != nil && *result.ContentLength > maxBytes {
		return nil, service.ErrGalleryInvalid
	}
	data, err := io.ReadAll(io.LimitReader(result.Body, maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read gallery object: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return nil, service.ErrGalleryInvalid
	}
	return data, nil
}

func (s *s3ImageStudioGalleryStore) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &key})
	if err != nil {
		return fmt.Errorf("gallery DeleteObject: %w", err)
	}
	return nil
}

func (s *s3ImageStudioGalleryStore) PresignGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	result, err := s3.NewPresignClient(s.client).PresignGetObject(ctx,
		&s3.GetObjectInput{Bucket: &s.bucket, Key: &key}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("gallery PresignGetObject: %w", err)
	}
	return result.URL, nil
}

func (s *s3ImageStudioGalleryStore) PresignOfficePut(ctx context.Context, key, mime string, size int64, checksum string, createOnly bool, expiry time.Duration) (string, http.Header, error) {
	input := &s3.PutObjectInput{
		Bucket: &s.bucket, Key: &key, ContentType: &mime, ContentLength: &size, ContentMD5: &checksum,
	}
	if createOnly {
		input.IfNoneMatch = aws.String("*")
	}
	result, err := s3.NewPresignClient(s.client).PresignPutObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", nil, err
	}
	headers := result.SignedHeader.Clone()
	headers.Del("Host")
	if headers.Get("Content-MD5") != checksum {
		return "", nil, fmt.Errorf("office upload checksum missing from signed headers")
	}
	headers.Set("Content-Type", mime)
	return result.URL, headers, nil
}
