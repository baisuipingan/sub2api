package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"time"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// StudioPostprocessMode names the existing browser post-processing behaviors.
// The generated original must be saved separately before calling this function.
type StudioPostprocessMode string

const (
	StudioPostprocessNone  StudioPostprocessMode = "none"
	StudioPostprocessMask  StudioPostprocessMode = "mask"
	StudioPostprocessKeyed StudioPostprocessMode = "keyed"
	// Generated results keep the pre-existing storage and spool bound.
	// Official reference-input limits are separate from result capacity.
	StudioMaxOutputImageBytes = 32 << 20
)

var errStudioImageInvalid = errors.New("invalid Studio image or post-processing request")

// ProcessStudioImage returns the final gallery image and its actual MIME type.
// source and mask are needed only for mask mode. It intentionally does not call
// an upstream model or accept arbitrary file paths/URLs from a request.
func ProcessStudioImage(
	ctx context.Context,
	raw, source, mask []byte,
	mode StudioPostprocessMode,
	outputFormat string,
	outputCompression *int,
) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if len(raw) == 0 || len(raw) > StudioMaxOutputImageBytes {
		return nil, "", errStudioImageInvalid
	}
	memoryBytes, err := studioProcessingMemoryRequirement(raw, source, mask, mode)
	if err != nil {
		return nil, "", err
	}
	releaseMemory, err := sharedStudioImageMemory.acquire(memoryBytes)
	if err != nil {
		return nil, "", err
	}
	defer releaseMemory()
	generated, originalMIME, err := decodeStudioImage(raw)
	if err != nil {
		return nil, "", err
	}
	if mode == StudioPostprocessNone {
		return raw, originalMIME, nil
	}

	var result *image.NRGBA
	switch mode {
	case StudioPostprocessMask:
		original, _, decodeErr := decodeStudioImage(source)
		if decodeErr != nil {
			return nil, "", fmt.Errorf("decode mask source: %w", decodeErr)
		}
		maskImage, _, decodeErr := decodeStudioImage(mask)
		if decodeErr != nil {
			return nil, "", fmt.Errorf("decode mask: %w", decodeErr)
		}
		if original.Bounds().Dx() != maskImage.Bounds().Dx() || original.Bounds().Dy() != maskImage.Bounds().Dy() {
			return nil, "", errStudioImageInvalid
		}
		result = compositeStudioMask(original, generated, maskImage)
	case StudioPostprocessKeyed:
		result = generated
		removeStudioKeyedBackground(result)
	default:
		return nil, "", errStudioImageInvalid
	}

	return encodeStudioImage(ctx, result, mode, outputFormat, outputCompression)
}

func studioImageConfig(data []byte) (image.Config, string, error) {
	if len(data) == 0 || len(data) > GalleryMaxImageBytes {
		return image.Config{}, "", errStudioImageInvalid
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	if _, valid := studioImagePixels(config.Width, config.Height); err != nil || !valid {
		return image.Config{}, "", errStudioImageInvalid
	}
	mime := ""
	switch format {
	case "png":
		mime = "image/png"
	case "jpeg":
		mime = "image/jpeg"
	case "webp":
		mime = "image/webp"
	default:
		return image.Config{}, "", errStudioImageInvalid
	}
	return config, mime, nil
}

// Reserve all simultaneously live pixels before decoding any of them. Mask
// compositing also keeps an output and possibly a resized generated image;
// keyed removal has per-pixel queue/coverage arrays. These are server resource
// costs, not official input-resolution restrictions.
func studioProcessingMemoryRequirement(raw, source, mask []byte, mode StudioPostprocessMode) (int64, error) {
	inputs := []struct {
		data   []byte
		weight int64
	}{{raw, 8}}
	switch mode {
	case StudioPostprocessNone:
	case StudioPostprocessMask:
		inputs = append(inputs, struct {
			data   []byte
			weight int64
		}{source, 16}, struct {
			data   []byte
			weight int64
		}{mask, 8})
	case StudioPostprocessKeyed:
		inputs[0].weight = 16
	default:
		return 0, errStudioImageInvalid
	}
	var total int64
	for _, input := range inputs {
		config, _, err := studioImageConfig(input.data)
		if err != nil {
			return 0, err
		}
		estimate, err := studioImageMemoryEstimate(config.Width, config.Height, input.weight)
		if err != nil || estimate > studioImageMemoryLimit-total {
			return 0, ErrGalleryBusy
		}
		total += estimate
	}
	return total, nil
}

// Called only inside ProcessStudioImage's retained memory reservation.
func decodeStudioImage(data []byte) (*image.NRGBA, string, error) {
	config, mime, err := studioImageConfig(data)
	if err != nil {
		return nil, "", err
	}
	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", errStudioImageInvalid
	}
	result := image.NewNRGBA(image.Rect(0, 0, config.Width, config.Height))
	draw.Draw(result, result.Bounds(), decoded, decoded.Bounds().Min, draw.Src)
	return result, mime, nil
}

func compositeStudioMask(source, generated, mask *image.NRGBA) *image.NRGBA {
	width, height := source.Bounds().Dx(), source.Bounds().Dy()
	if generated.Bounds().Dx() != width || generated.Bounds().Dy() != height {
		resized := image.NewNRGBA(image.Rect(0, 0, width, height))
		xdraw.CatmullRom.Scale(resized, resized.Bounds(), generated, generated.Bounds(), draw.Src, nil)
		generated = resized
	}
	result := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			o := result.PixOffset(x, y)
			s := source.PixOffset(x, y)
			g := generated.PixOffset(x, y)
			m := mask.PixOffset(x, y)
			preserve := float64(mask.Pix[m+3]) / 255
			for c := 0; c < 4; c++ {
				result.Pix[o+c] = studioByte(float64(source.Pix[s+c])*preserve + float64(generated.Pix[g+c])*(1-preserve))
			}
		}
	}
	return result
}

type studioRGB struct{ r, g, b float64 }

var (
	studioGreen   = studioRGB{0, 255, 0}
	studioMagenta = studioRGB{255, 0, 255}
)

func removeStudioKeyedBackground(img *image.NRGBA) {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	count := width * height
	key := detectStudioKeyColor(img)
	background := make([]byte, count)
	visited := make([]byte, count)
	queue := make([]uint32, count)
	start, end := 0, 0

	enqueueBorder := func(index int) {
		if visited[index] != 0 {
			return
		}
		visited[index] = 1
		if studioBackgroundConfidence(img, index, key) < 0.18 {
			return
		}
		background[index] = 1
		queue[end] = uint32(index)
		end++
	}
	for x := 0; x < width; x++ {
		enqueueBorder(x)
		enqueueBorder((height-1)*width + x)
	}
	for y := 1; y < height-1; y++ {
		enqueueBorder(y * width)
		enqueueBorder(y*width + width - 1)
	}
	for start < end {
		index := int(queue[start])
		start++
		studioNeighbors(index, width, height, func(next int) {
			if visited[next] != 0 {
				return
			}
			visited[next] = 1
			if studioBackgroundConfidence(img, next, key) >= 0.18 {
				background[next] = 1
				queue[end] = uint32(next)
				end++
			}
		})
	}

	// Isolated key-colored areas inside a subject are treated like the browser's
	// original algorithm, while non-key-colored holes remain opaque.
	for seed := 0; seed < count; seed++ {
		if background[seed] != 0 || visited[seed] != 0 || studioBackgroundConfidence(img, seed, key) < 0.68 {
			continue
		}
		start, end = 0, 1
		visited[seed] = 1
		queue[0] = uint32(seed)
		var total float64
		strict, strong := 0, 0
		for start < end {
			index := int(queue[start])
			start++
			confidence := studioBackgroundConfidence(img, index, key)
			total += confidence
			if confidence >= 0.68 {
				strict++
			}
			if confidence >= 0.86 {
				strong++
			}
			studioNeighbors(index, width, height, func(next int) {
				if background[next] != 0 || visited[next] != 0 || studioBackgroundConfidence(img, next, key) < 0.24 {
					return
				}
				visited[next] = 1
				queue[end] = uint32(next)
				end++
			})
		}
		average := total / float64(end)
		if average >= 0.42 || float64(strict)/float64(end) >= 0.18 || float64(strong)/float64(end) >= 0.05 || (end <= 3 && average >= 0.34) {
			for _, index := range queue[:end] {
				background[index] = 1
			}
		}
	}

	distance := studioDistanceToBackground(background, queue, width, height)
	for index := 0; index < count; index++ {
		o := index * 4
		red, green, blue := float64(img.Pix[o]), float64(img.Pix[o+1]), float64(img.Pix[o+2])
		confidence := studioBackgroundConfidence(img, index, key)
		alpha := 255.0
		if background[index] != 0 {
			alpha = 0
		} else if distance[index] > 0 {
			transparency := studioEdgeTransparency(red, green, blue, confidence, int(distance[index]), key)
			if transparency > 0 {
				alpha = math.Round(255 * (1 - transparency))
			}
			minimum := 196.0
			switch distance[index] {
			case 1:
				minimum = 48
			case 2:
				minimum = 128
			}
			alpha = math.Max(alpha, minimum)
		} else {
			spill := studioKeyChannelMix(red, green, blue, key)
			if confidence >= 0.46 && spill >= 0.45 {
				alpha = math.Max(math.Round(255*(1-spill*0.75)), 96)
			}
		}
		if alpha > 0 {
			edgeStrength := 0.0
			switch distance[index] {
			case 0:
				if confidence >= 0.46 {
					edgeStrength = 0.35
				}
			case 1:
				edgeStrength = 0.55
			case 2:
				edgeStrength = 0.32
			default:
				edgeStrength = 0.16
			}
			spillMix := studioKeyChannelMix(red, green, blue, key) * edgeStrength
			backgroundMix := studioClamp01(math.Max((255-alpha)/255, math.Max((confidence-0.1)/0.9*edgeStrength, spillMix)))
			if backgroundMix > 0 {
				foregroundMix := math.Max(0.08, 1-backgroundMix)
				red = (red - key.r*backgroundMix) / foregroundMix
				green = (green - key.g*backgroundMix) / foregroundMix
				blue = (blue - key.b*backgroundMix) / foregroundMix
			}
		}
		img.Pix[o], img.Pix[o+1], img.Pix[o+2], img.Pix[o+3] = studioByte(red), studioByte(green), studioByte(blue), studioByte(alpha)
	}
}

func detectStudioKeyColor(img *image.NRGBA) studioRGB {
	width, height := img.Bounds().Dx(), img.Bounds().Dy()
	greenScore, magentaScore := 0, 0
	score := func(index int) {
		o := index * 4
		r, g, b := float64(img.Pix[o]), float64(img.Pix[o+1]), float64(img.Pix[o+2])
		if studioColorDistance(r, g, b, studioGreen) < 100 {
			greenScore++
		}
		if studioColorDistance(r, g, b, studioMagenta) < 100 {
			magentaScore++
		}
	}
	for x := 0; x < width; x++ {
		score(x)
		score((height-1)*width + x)
	}
	for y := 1; y < height-1; y++ {
		score(y * width)
		score(y*width + width - 1)
	}
	if magentaScore > greenScore {
		return studioMagenta
	}
	return studioGreen
}

func studioColorDistance(r, g, b float64, key studioRGB) float64 {
	return math.Sqrt((r-key.r)*(r-key.r) + (g-key.g)*(g-key.g) + (b-key.b)*(b-key.b))
}

func studioBackgroundConfidence(img *image.NRGBA, index int, key studioRGB) float64 {
	o := index * 4
	return studioClamp01((150 - studioColorDistance(float64(img.Pix[o]), float64(img.Pix[o+1]), float64(img.Pix[o+2]), key)) / 150)
}

func studioNeighbors(index, width, height int, visit func(int)) {
	x, y := index%width, index/width
	if x > 0 {
		visit(index - 1)
	}
	if x < width-1 {
		visit(index + 1)
	}
	if y > 0 {
		visit(index - width)
	}
	if y < height-1 {
		visit(index + width)
	}
}

func studioDistanceToBackground(background []byte, queue []uint32, width, height int) []byte {
	distance := make([]byte, len(background))
	end := 0
	for index := range background {
		if background[index] != 0 {
			continue
		}
		studioNeighbors(index, width, height, func(next int) {
			if background[next] != 0 && distance[index] == 0 {
				distance[index] = 1
				queue[end] = uint32(index)
				end++
			}
		})
	}
	start := 0
	for current := byte(1); current < 4 && start < end; current++ {
		layerEnd := end
		for start < layerEnd {
			index := int(queue[start])
			start++
			studioNeighbors(index, width, height, func(next int) {
				if background[next] == 0 && distance[next] == 0 {
					distance[next] = current + 1
					queue[end] = uint32(next)
					end++
				}
			})
		}
	}
	return distance
}

func studioEdgeTransparency(r, g, b, confidence float64, distance int, key studioRGB) float64 {
	strength := 0.25
	switch distance {
	case 1:
		strength = 1
	case 2:
		strength = 0.75
	case 3:
		strength = 0.45
	}
	distanceEstimate := studioClamp01((confidence - 0.08) / 0.84 * strength)
	channelEstimate := studioKeyChannelMix(r, g, b, key) * strength
	return studioClamp01(math.Max(distanceEstimate, channelEstimate))
}

func studioKeyChannelMix(r, g, b float64, key studioRGB) float64 {
	if key.g == 255 {
		return studioClamp01((g - math.Min(r, b)) / 255)
	}
	return studioClamp01((math.Min(r, b) - g*0.65) / 255)
}

func studioClamp01(value float64) float64 { return math.Max(0, math.Min(1, value)) }
func studioByte(value float64) byte       { return byte(math.Max(0, math.Min(255, math.Round(value)))) }

func encodeStudioImage(ctx context.Context, img *image.NRGBA, mode StudioPostprocessMode, format string, compression *int) ([]byte, string, error) {
	var out bytes.Buffer
	switch format {
	case "png":
		err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, img)
		if err != nil {
			return nil, "", err
		}
		return boundedStudioOutput(out.Bytes(), "image/png")
	case "jpeg":
		if mode == StudioPostprocessKeyed {
			return nil, "", errStudioImageInvalid
		}
		quality := studioJPEGQuality(compression)
		if err := jpeg.Encode(&out, img, &jpeg.Options{Quality: quality}); err != nil {
			return nil, "", err
		}
		return boundedStudioOutput(out.Bytes(), "image/jpeg")
	case "webp":
		quality := 75
		if compression != nil {
			quality = *compression
			if mode == StudioPostprocessKeyed {
				quality = 100 - quality
			}
		}
		if quality < 0 || quality > 100 {
			return nil, "", errStudioImageInvalid
		}
		return encodeStudioWebP(ctx, img, quality)
	default:
		return nil, "", errStudioImageInvalid
	}
}

func studioJPEGQuality(compression *int) int {
	if compression == nil {
		return 90
	}
	return max(1, min(100, *compression))
}

func boundedStudioOutput(data []byte, mime string) ([]byte, string, error) {
	if len(data) == 0 || len(data) > StudioMaxOutputImageBytes {
		return nil, "", errStudioImageInvalid
	}
	return data, mime, nil
}

// cwebp is provided by Alpine's libwebp-tools package in the runtime image.
// File names are server-generated; no client value reaches exec arguments.
func encodeStudioWebP(ctx context.Context, img *image.NRGBA, quality int) ([]byte, string, error) {
	input, err := os.CreateTemp("", "studio-encode-*.png")
	if err != nil {
		return nil, "", err
	}
	inputPath := input.Name()
	defer func() { _ = os.Remove(inputPath) }()
	if err := png.Encode(input, img); err != nil {
		_ = input.Close()
		return nil, "", err
	}
	if err := input.Close(); err != nil {
		return nil, "", err
	}
	output, err := os.CreateTemp("", "studio-encode-*.webp")
	if err != nil {
		return nil, "", err
	}
	outputPath := output.Name()
	defer func() { _ = os.Remove(outputPath) }()
	if err := output.Close(); err != nil {
		return nil, "", err
	}
	encodeCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(encodeCtx, "cwebp", "-quiet", "-q", strconv.Itoa(quality), inputPath, "-o", outputPath)
	if err := cmd.Run(); err != nil {
		return nil, "", fmt.Errorf("studio WebP encoding failed: %w", err)
	}
	file, err := os.Open(outputPath)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = file.Close() }()
	data, err := io.ReadAll(io.LimitReader(file, StudioMaxOutputImageBytes+1))
	if err != nil {
		return nil, "", err
	}
	return boundedStudioOutput(data, "image/webp")
}
