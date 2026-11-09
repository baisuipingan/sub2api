package service

import (
	"math"
	"sync"
)

// This is a process resource budget, not a provider image-resolution limit.
// Headers are inspected before decode so compressed images cannot cause an
// unbounded pixel allocation. Original bytes and dimensions are not changed.
const studioImageMemoryLimit int64 = 512 << 20
const studioImageMemoryOverhead int64 = 32 << 20

type studioImageMemoryBudget struct {
	mu   sync.Mutex
	used int64
}

var sharedStudioImageMemory studioImageMemoryBudget

func studioImagePixels(width, height int) (int64, bool) {
	if width <= 0 || height <= 0 || int64(width) > math.MaxInt64/int64(height) {
		return 0, false
	}
	return int64(width) * int64(height), true
}

func studioImageMemoryEstimate(width, height int, bytesPerPixel int64) (int64, error) {
	pixels, valid := studioImagePixels(width, height)
	if !valid || bytesPerPixel <= 0 || pixels > (math.MaxInt64-studioImageMemoryOverhead)/bytesPerPixel {
		return 0, ErrGalleryBusy
	}
	return pixels*bytesPerPixel + studioImageMemoryOverhead, nil
}

func (b *studioImageMemoryBudget) acquire(bytes int64) (func(), error) {
	b.mu.Lock()
	if bytes <= 0 || bytes > studioImageMemoryLimit-b.used {
		b.mu.Unlock()
		return nil, ErrGalleryBusy
	}
	b.used += bytes
	b.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { b.mu.Lock(); b.used -= bytes; b.mu.Unlock() }) }, nil
}

func acquireStudioImageMemory(width, height int) (func(), error) {
	bytes, err := studioImageMemoryEstimate(width, height, 8)
	if err != nil {
		return nil, err
	}
	return sharedStudioImageMemory.acquire(bytes)
}

// StudioMaskProcessingCapacity rejects a mask operation that cannot fit even
// when this instance is otherwise idle, before a paid model call is admitted.
// Source and mask share dimensions; the actual generated image is checked
// again by ProcessStudioImage together with all live working buffers.
func StudioMaskProcessingCapacity(width, height int) error {
	return ValidateStudioMaskProcessingCapacity(width, height, 0, 0)
}

// ValidateStudioMaskProcessingCapacity also accounts for known generated
// output dimensions. Passing both output dimensions as zero requests only the
// lower bound when no output size is available during admission.
func ValidateStudioMaskProcessingCapacity(width, height, outputWidth, outputHeight int) error {
	minimum, err := studioImageMemoryEstimate(width, height, 24)
	if err != nil || minimum > studioImageMemoryLimit-2*studioImageMemoryOverhead {
		return ErrGalleryBusy
	}
	if outputWidth != 0 || outputHeight != 0 {
		output, err := studioImageMemoryEstimate(outputWidth, outputHeight, 8)
		if err != nil || output-studioImageMemoryOverhead > studioImageMemoryLimit-minimum-2*studioImageMemoryOverhead {
			return ErrGalleryBusy
		}
	}
	return nil
}
