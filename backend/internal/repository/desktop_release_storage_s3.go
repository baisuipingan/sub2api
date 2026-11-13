package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/util/urlvalidator"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
)

type desktopReleaseS3Store struct {
	client *s3.Client
	bucket string
}

func NewDesktopReleaseObjectStoreFactory() service.DesktopReleaseObjectStoreFactory {
	return func(ctx context.Context, cfg *service.BackupS3Config) (service.DesktopReleaseObjectStore, error) {
		if cfg.Endpoint != "" {
			if _, err := urlvalidator.ValidateHTTPSURL(cfg.Endpoint, urlvalidator.ValidationOptions{AllowPrivate: false}); err != nil {
				return nil, err
			}
		}
		client, err := newS3Client(ctx, s3ClientParams{Endpoint: cfg.Endpoint, Region: cfg.Region, AccessKeyID: cfg.AccessKeyID, SecretAccessKey: cfg.SecretAccessKey, ForcePathStyle: cfg.ForcePathStyle})
		if err != nil {
			return nil, err
		}
		shared, err := httpclient.GetClient(httpclient.Options{Timeout: 10 * time.Second, ValidateResolvedIP: true})
		if err != nil {
			return nil, err
		}
		transport := *shared
		// In particular never forward S3 authorization on a provider redirect.
		transport.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		options := client.Options()
		options.HTTPClient = &transport
		client = s3.New(options)
		return &desktopReleaseS3Store{client: client, bucket: cfg.Bucket}, nil
	}
}

func (s *desktopReleaseS3Store) Head(ctx context.Context, key string) (*service.DesktopReleaseObjectHead, error) {
	result, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		var api smithy.APIError
		if errors.As(err, &api) && (api.ErrorCode() == "NotFound" || api.ErrorCode() == "NoSuchKey" || api.ErrorCode() == "404") {
			return &service.DesktopReleaseObjectHead{Exists: false}, nil
		}
		return nil, err
	}
	return &service.DesktopReleaseObjectHead{Exists: true, Size: aws.ToInt64(result.ContentLength), SHA512: result.Metadata["sha512"], Version: result.Metadata["milocode-version"], ETag: aws.ToString(result.ETag)}, nil
}

// The SDK intentionally removes Content-Type for generic S3 presigning. At this
// narrow upload boundary it is part of the frozen release plan, so restore it
// before the standard AWS v4 signer computes the signature. No custom crypto.
type desktopReleasePresigner struct {
	signer      *v4.Signer
	contentType string
	size        int64
}

func (s desktopReleasePresigner) PresignHTTP(ctx context.Context, credentials aws.Credentials, req *http.Request, payloadHash, serviceName, region string, signingTime time.Time, opts ...func(*v4.SignerOptions)) (string, http.Header, error) {
	req.Header.Set("Content-Type", s.contentType)
	req.ContentLength = s.size
	opts = append(opts, func(options *v4.SignerOptions) { options.DisableHeaderHoisting = true })
	return s.signer.PresignHTTP(ctx, credentials, req, payloadHash, serviceName, region, signingTime, opts...)
}

func (s *desktopReleaseS3Store) PresignUpload(ctx context.Context, in service.DesktopReleaseUploadRequest, cache string, expiry time.Duration) (*service.DesktopReleaseUpload, error) {
	input := &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(in.Key), ContentLength: aws.Int64(in.Size), ContentType: aws.String(in.ContentType), CacheControl: aws.String(cache), Metadata: map[string]string{"sha256": in.SHA256, "sha512": in.SHA512, "milocode-version": in.Version}}
	if in.IfMatch != "" {
		input.IfMatch = aws.String(in.IfMatch)
	}
	if in.IfNoneMatch != "" {
		input.IfNoneMatch = aws.String(in.IfNoneMatch)
	}
	presigner := s3.NewPresignClient(s.client, func(options *s3.PresignOptions) {
		options.Presigner = desktopReleasePresigner{signer: v4.NewSigner(), contentType: in.ContentType, size: in.Size}
	})
	result, err := presigner.PresignPutObject(ctx, input, s3.WithPresignExpires(expiry))
	if err != nil {
		return nil, err
	}
	headers := make(map[string]string)
	for key, values := range result.SignedHeader {
		if strings.EqualFold(key, "Host") {
			continue
		}
		if strings.EqualFold(key, "Authorization") || strings.EqualFold(key, "Cookie") || len(values) != 1 {
			return nil, fmt.Errorf("unexpected release upload signing header")
		}
		headers[http.CanonicalHeaderKey(key)] = values[0]
	}
	// Content-Length participates in SigV4 through req.ContentLength and may not
	// appear in SignedHeader. It is still mandatory for the streaming uploader.
	headers["Content-Length"] = strconv.FormatInt(in.Size, 10)
	return &service.DesktopReleaseUpload{URL: result.URL, Headers: headers, ExpiresAt: time.Now().UTC().Add(expiry)}, nil
}

func (s *desktopReleaseS3Store) PutProbe(ctx context.Context, key string, body []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key), Body: bytes.NewReader(body), ContentLength: aws.Int64(int64(len(body))), ContentType: aws.String("text/plain"), CacheControl: aws.String("no-store"), IfNoneMatch: aws.String("*")})
	return err
}
func (s *desktopReleaseS3Store) GetProbe(ctx context.Context, key string) ([]byte, error) {
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	if err != nil {
		return nil, err
	}
	defer func() { _ = result.Body.Close() }()
	return io.ReadAll(io.LimitReader(result.Body, 4097))
}
func (s *desktopReleaseS3Store) DeleteProbe(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: aws.String(key)})
	return err
}
