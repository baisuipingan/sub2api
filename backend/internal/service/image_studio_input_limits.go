package service

import (
	"encoding/json"
	"strconv"
	"strings"
)

// OpenAI's image-edit endpoint documents file limits and a reference count,
// not a 20MP/4096px limit for ordinary reference images.
// https://developers.openai.com/api/reference/resources/images/methods/edit
const (
	studioOpenAIImageByteLimit = 50_000_000 // strictly less than 50 MB
	studioOpenAIMaskByteLimit  = 4_000_000  // endpoint mask field: strictly less than 4 MB
	studioOpenAIReferenceLimit = 16
)

type StudioInputValidationError struct {
	Reason  string
	Message string
}

func (e *StudioInputValidationError) Error() string { return e.Message }
func (e *StudioInputValidationError) Unwrap() error { return ErrStudioJobInvalid }

func studioOpenAIInputRules(spec StudioJobSpec) bool {
	if spec.Protocol != "openai" {
		return false
	}
	if len(spec.AdmissionProfiles) == 0 {
		return true
	}
	for _, profile := range spec.AdmissionProfiles {
		if profile.Provider != "google" && profile.Provider != "xai" {
			return true
		}
	}
	return false
}

func validateStudioInputFile(mime string, bytes int64, mask bool) error {
	if mask {
		if mime != "image/png" || bytes <= 0 || bytes >= studioOpenAIMaskByteLimit {
			return &StudioInputValidationError{"STUDIO_MASK_INVALID", "OpenAI mask must be a PNG smaller than 4 MB and match the first reference image dimensions"}
		}
		return nil
	}
	if bytes <= 0 || bytes >= studioOpenAIImageByteLimit {
		return &StudioInputValidationError{"STUDIO_REFERENCE_TOO_LARGE", "each OpenAI reference image must be smaller than 50 MB"}
	}
	if mime != "image/png" && mime != "image/jpeg" && mime != "image/webp" {
		return &StudioInputValidationError{"STUDIO_REFERENCE_FORMAT", "OpenAI reference images must be PNG, JPEG or WebP"}
	}
	return nil
}

func validateStudioInputCount(spec StudioJobSpec) error {
	if studioOpenAIInputRules(spec) && (len(spec.InputImageIDs) > studioOpenAIReferenceLimit || len(spec.RequestInputImageIDs) > studioOpenAIReferenceLimit) {
		return &StudioInputValidationError{"STUDIO_REFERENCE_COUNT", "OpenAI accepts at most 16 reference images per edit"}
	}
	return nil
}

func validateStudioInputAssets(spec StudioJobSpec, assets map[string]*GalleryAsset) error {
	if !studioOpenAIInputRules(spec) {
		return nil
	}
	if err := validateStudioInputCount(spec); err != nil {
		return err
	}
	for _, id := range append(append([]string{}, spec.InputImageIDs...), spec.RequestInputImageIDs...) {
		asset := assets[id]
		if asset == nil {
			return ErrStudioJobInvalid
		}
		if err := validateStudioInputFile(asset.MIME, asset.Bytes, false); err != nil {
			return err
		}
	}
	if spec.MaskImageID != "" {
		mask := assets[spec.MaskImageID]
		if mask == nil || len(spec.RequestInputImageIDs) == 0 {
			return ErrStudioJobInvalid
		}
		if err := validateStudioInputFile(mask.MIME, mask.Bytes, true); err != nil {
			return err
		}
		first := assets[spec.RequestInputImageIDs[0]]
		if first == nil || first.Width != mask.Width || first.Height != mask.Height {
			return &StudioInputValidationError{"STUDIO_MASK_INVALID", "OpenAI mask dimensions must match the first reference image"}
		}
	}
	return nil
}

func validateStudioMaskMemory(spec StudioJobSpec, source *GalleryAsset) error {
	width, height := source.Width, source.Height
	var params struct {
		Size string `json:"size"`
	}
	if json.Unmarshal(spec.Params, &params) == nil {
		if parts := strings.Split(params.Size, "x"); len(parts) == 2 {
			w, wErr := strconv.Atoi(parts[0])
			h, hErr := strconv.Atoi(parts[1])
			if wErr == nil && hErr == nil && w > 0 && h > 0 {
				width, height = w, h
			}
		}
	}
	if err := ValidateStudioMaskProcessingCapacity(source.Width, source.Height, width, height); err != nil {
		return &StudioInputValidationError{"STUDIO_PROCESSING_CAPACITY", "this masked edit exceeds the server's image-processing capacity; use a smaller edit source; no model call was made"}
	}
	return nil
}
