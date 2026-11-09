//go:build unit

package service

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGalleryUploadValidationExplainsFailureBeforeStorage(t *testing.T) {
	valid := galleryPNG(t, 4, 3)
	for _, tc := range []struct {
		name, id, mime, reason string
		data                   []byte
	}{
		{"invalid id", "unsafe.id", "image/png", "GALLERY_INVALID_ID", valid},
		{"empty", "input", "image/png", "GALLERY_IMAGE_EMPTY", nil},
		{"unsupported", "input", "image/svg+xml", "GALLERY_IMAGE_FORMAT_UNSUPPORTED", []byte(`<svg>private-test-text</svg>`)},
		{"pixels", "input", "image/png", "GALLERY_IMAGE_TOO_MANY_PIXELS", galleryPNGHeader(5000, 5000)},
		{"mismatched type", "input", "image/jpeg", "GALLERY_IMAGE_MIME_MISMATCH", valid},
		{"broken pixels", "input", "image/png", "GALLERY_IMAGE_DECODE_FAILED", galleryPNGHeader(4, 3)},
		{"oversized", "input", "image/png", "GALLERY_TOO_LARGE", make([]byte, GalleryMaxImageBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, store, _ := galleryTestService(t)
			err := svc.PutAsset(context.Background(), 17, tc.id, tc.mime, tc.data)
			require.ErrorIs(t, err, ErrGalleryInvalid)
			var detail *GalleryValidationError
			require.True(t, errors.As(err, &detail))
			require.Equal(t, tc.reason, detail.Reason)
			require.NotContains(t, err.Error(), "private-test-text")
			require.Zero(t, repo.reserveCalls)
			require.Empty(t, store.objects)
			if tc.reason == "GALLERY_IMAGE_TOO_MANY_PIXELS" {
				require.Equal(t, map[string]string{"width": "5000", "height": "5000", "max_pixels": "20000000"}, detail.Metadata)
			}
		})
	}
}

func TestGalleryUploadCanonicalizesJPEGTypeWithoutChangingBytes(t *testing.T) {
	var source bytes.Buffer
	require.NoError(t, jpeg.Encode(&source, image.NewRGBA(image.Rect(0, 0, 4, 3)), nil))
	for _, declared := range []string{"image/jpeg", "image/jpg", "IMAGE/JPEG; charset=binary", "image/pjpeg", ""} {
		t.Run(declared, func(t *testing.T) {
			svc, repo, store, _ := galleryTestService(t)
			require.NoError(t, svc.PutAsset(context.Background(), 17, "reference", declared, source.Bytes()))
			asset := repo.assets["reference"]
			require.Equal(t, "image/jpeg", asset.MIME)
			require.Equal(t, source.Bytes(), store.objects[asset.OriginalKey])
		})
	}
}
