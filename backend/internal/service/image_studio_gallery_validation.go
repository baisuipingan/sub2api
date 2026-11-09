package service

import (
	"fmt"
	"mime"
	"net/http"
	"strings"
)

// GalleryValidationError describes safe facts about a rejected upload. It keeps
// ErrGalleryInvalid compatibility without exposing decoder messages or bytes.
type GalleryValidationError struct {
	Reason   string
	Message  string
	Metadata map[string]string
}

func (e *GalleryValidationError) Error() string { return e.Message }
func (e *GalleryValidationError) Unwrap() error { return ErrGalleryInvalid }

func galleryValidationError(reason, message string, metadata map[string]string) error {
	return &GalleryValidationError{Reason: reason, Message: message, Metadata: metadata}
}

// GalleryDeclaredImageMIME returns a canonical allowlisted value for validation
// and diagnostic logs. Arbitrary client header values never enter either log.
func GalleryDeclaredImageMIME(contentType string) string {
	value, _, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		return ""
	}
	switch strings.ToLower(value) {
	case "image/jpg", "image/pjpeg", "image/jpeg":
		return "image/jpeg"
	case "image/png":
		return "image/png"
	case "image/webp":
		return "image/webp"
	case "image/gif":
		return "image/gif"
	default:
		return ""
	}
}

func galleryImageDecodeError(data []byte) error {
	if GalleryDeclaredImageMIME(http.DetectContentType(data)) != "" {
		return galleryValidationError("GALLERY_IMAGE_DECODE_FAILED", "image data is incomplete or cannot be decoded; export the image again and retry", nil)
	}
	return galleryValidationError("GALLERY_IMAGE_FORMAT_UNSUPPORTED", "unsupported image format; upload a PNG, JPEG, WebP or GIF image", nil)
}

func galleryImageDimensions(width, height int) map[string]string {
	return map[string]string{"width": fmt.Sprint(width), "height": fmt.Sprint(height), "max_pixels": fmt.Sprint(galleryMaxPixels)}
}
