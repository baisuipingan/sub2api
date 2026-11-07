package service

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/require"
)

func studioTestPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var out bytes.Buffer
	require.NoError(t, png.Encode(&out, img))
	return out.Bytes()
}

func studioTestPixel(t *testing.T, data []byte, x, y int) color.NRGBA {
	t.Helper()
	decoded, _, err := image.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
}

func TestProcessStudioImagePreservesOriginalWhenNoPostprocess(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 1))
	img.SetNRGBA(0, 0, color.NRGBA{R: 210, G: 12, B: 40, A: 255})
	original := studioTestPNG(t, img)
	got, mime, err := ProcessStudioImage(context.Background(), original, nil, nil, StudioPostprocessNone, "webp", nil)
	require.NoError(t, err)
	require.Equal(t, original, got)
	require.Equal(t, "image/png", mime)
}

func TestProcessStudioImageCompositesOnlyMaskedRegion(t *testing.T) {
	source := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	generated := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	mask := image.NewNRGBA(image.Rect(0, 0, 3, 1))
	for x := 0; x < 3; x++ {
		source.SetNRGBA(x, 0, color.NRGBA{R: 240, A: 255})
		generated.SetNRGBA(x, 0, color.NRGBA{B: 240, A: 255})
	}
	mask.SetNRGBA(0, 0, color.NRGBA{A: 255})
	mask.SetNRGBA(1, 0, color.NRGBA{A: 0})
	mask.SetNRGBA(2, 0, color.NRGBA{A: 128})
	got, mime, err := ProcessStudioImage(context.Background(), studioTestPNG(t, generated), studioTestPNG(t, source), studioTestPNG(t, mask), StudioPostprocessMask, "png", nil)
	require.NoError(t, err)
	require.Equal(t, "image/png", mime)
	require.Equal(t, color.NRGBA{R: 240, A: 255}, studioTestPixel(t, got, 0, 0))
	require.Equal(t, color.NRGBA{B: 240, A: 255}, studioTestPixel(t, got, 1, 0))
	require.Equal(t, color.NRGBA{R: 120, B: 120, A: 255}, studioTestPixel(t, got, 2, 0))
}

func TestProcessStudioImageKeyedBackgroundAndInteriorIsland(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  color.NRGBA
		face color.NRGBA
	}{
		{name: "green", key: color.NRGBA{G: 255, A: 255}, face: color.NRGBA{R: 240, A: 255}},
		{name: "magenta", key: color.NRGBA{R: 255, B: 255, A: 255}, face: color.NRGBA{G: 220, A: 255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := image.NewNRGBA(image.Rect(0, 0, 7, 7))
			for y := 0; y < 7; y++ {
				for x := 0; x < 7; x++ {
					img.SetNRGBA(x, y, tc.key)
					if x >= 2 && x <= 4 && y >= 2 && y <= 4 {
						img.SetNRGBA(x, y, tc.face)
					}
				}
			}
			img.SetNRGBA(3, 3, tc.key) // enclosed pure-key island
			got, mime, err := ProcessStudioImage(context.Background(), studioTestPNG(t, img), nil, nil, StudioPostprocessKeyed, "png", nil)
			require.NoError(t, err)
			require.Equal(t, "image/png", mime)
			require.Zero(t, studioTestPixel(t, got, 0, 0).A)
			require.Zero(t, studioTestPixel(t, got, 3, 3).A)
			require.Equal(t, tc.face, studioTestPixel(t, got, 2, 2))
		})
	}
}

func TestProcessStudioImageRejectsInvalidMaskAndFormat(t *testing.T) {
	input := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	other := image.NewNRGBA(image.Rect(0, 0, 1, 1))
	data := studioTestPNG(t, input)
	_, _, err := ProcessStudioImage(context.Background(), data, data, studioTestPNG(t, other), StudioPostprocessMask, "png", nil)
	require.Error(t, err)
	_, _, err = ProcessStudioImage(context.Background(), data, nil, nil, StudioPostprocessKeyed, "jpeg", nil)
	require.Error(t, err)
	_, _, err = ProcessStudioImage(context.Background(), data, nil, nil, StudioPostprocessMode("unexpected"), "png", nil)
	require.Error(t, err)
	_, _, err = ProcessStudioImage(context.Background(), []byte("not an image"), nil, nil, StudioPostprocessNone, "png", nil)
	require.Error(t, err)
}

func TestProcessStudioImageWebPOutput(t *testing.T) {
	if _, err := exec.LookPath("cwebp"); err != nil {
		t.Skip("libwebp-tools is supplied by the runtime image")
	}
	img := image.NewNRGBA(image.Rect(0, 0, 5, 5))
	for y := 0; y < 5; y++ {
		for x := 0; x < 5; x++ {
			img.SetNRGBA(x, y, color.NRGBA{G: 255, A: 255})
		}
	}
	img.SetNRGBA(2, 2, color.NRGBA{R: 255, A: 255})
	got, mime, err := ProcessStudioImage(context.Background(), studioTestPNG(t, img), nil, nil, StudioPostprocessKeyed, "webp", nil)
	require.NoError(t, err)
	require.Equal(t, "image/webp", mime)
	config, format, err := image.DecodeConfig(bytes.NewReader(got))
	require.NoError(t, err)
	require.Equal(t, "webp", format)
	require.Equal(t, 5, config.Width)
	require.Equal(t, 5, config.Height)
}
