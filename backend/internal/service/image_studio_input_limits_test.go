package service

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStudioOpenAIReferenceUsesOfficialFileLimitsWithoutPixelResize(t *testing.T) {
	spec := StudioJobSpec{Protocol: "openai", InputImageIDs: []string{"original"}, RequestInputImageIDs: []string{"original"}}
	assets := map[string]*GalleryAsset{"original": {MIME: "image/jpeg", Bytes: 1_741_565, Width: 9000, Height: 4367}}
	require.NoError(t, validateStudioInputAssets(spec, assets), "a normal 39MP JPEG is not subject to an invented model pixel limit")
	assets["original"].Bytes = 49_999_999
	require.NoError(t, validateStudioInputAssets(spec, assets))
	assets["original"].Bytes = 50_000_000
	err := validateStudioInputAssets(spec, assets)
	var invalid *StudioInputValidationError
	require.ErrorAs(t, err, &invalid)
	require.Equal(t, "STUDIO_REFERENCE_TOO_LARGE", invalid.Reason)
	require.True(t, errors.Is(err, ErrStudioJobInvalid))
}

func TestStudioOpenAIReferenceCountFormatAndMaskConstraints(t *testing.T) {
	spec := StudioJobSpec{Protocol: "openai", InputImageIDs: []string{"first"}, RequestInputImageIDs: []string{"first"}}
	assets := map[string]*GalleryAsset{
		"first": {MIME: "image/jpeg", Bytes: 1000, Width: 9000, Height: 4367},
		"mask":  {MIME: "image/png", Bytes: 3_999_999, Width: 9000, Height: 4367},
	}
	spec.MaskImageID = "mask"
	require.NoError(t, validateStudioInputAssets(spec, assets))
	assets["mask"].Bytes = 4_000_000
	require.Error(t, validateStudioInputAssets(spec, assets))
	assets["mask"].Bytes = 1000
	assets["mask"].Width = 4096
	require.Error(t, validateStudioInputAssets(spec, assets), "mask must keep the reference dimensions")
	spec.MaskImageID = ""
	assets["first"].MIME = "image/gif"
	require.Error(t, validateStudioInputAssets(spec, assets))
	assets["first"].MIME = "image/webp"
	require.NoError(t, validateStudioInputAssets(spec, assets))
	spec.InputImageIDs = make([]string, 17)
	require.Error(t, validateStudioInputCount(spec))
	spec.InputImageIDs = make([]string, 16)
	require.NoError(t, validateStudioInputCount(spec))
}

func TestStudioDoesNotApplyOpenAIInputRulesToOtherProviderProtocols(t *testing.T) {
	for _, spec := range []StudioJobSpec{
		{Protocol: "gemini"}, {Protocol: "grok"},
		{Protocol: "openai", AdmissionProfiles: []ImageStudioModelProfile{{Provider: "google"}}},
		{Protocol: "openai", AdmissionProfiles: []ImageStudioModelProfile{{Provider: "xai"}}},
	} {
		require.NoError(t, validateStudioInputAssets(spec, nil))
	}
}

func TestStudioMaskMemoryAdmissionUsesRequestedOutputDimensions(t *testing.T) {
	source := &GalleryAsset{Width: 4000, Height: 4000}
	require.NoError(t, validateStudioMaskMemory(StudioJobSpec{Params: json.RawMessage(`{"size":"1024x1024"}`)}, source))
	for _, raw := range []string{`{"size":"4000x4000"}`, `{}`} {
		var invalid *StudioInputValidationError
		err := validateStudioMaskMemory(StudioJobSpec{Params: json.RawMessage(raw)}, source)
		require.ErrorAs(t, err, &invalid)
		require.Equal(t, "STUDIO_PROCESSING_CAPACITY", invalid.Reason)
	}
}
