package repository

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/aws/aws-sdk-go-v2/service/s3"
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
