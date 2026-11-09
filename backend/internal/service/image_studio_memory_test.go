//go:build unit

package service

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStudioImageMemoryAllowsReportedReferenceWithoutResizing(t *testing.T) {
	estimate, err := studioImageMemoryEstimate(9000, 4367, 8)
	require.NoError(t, err)
	require.Equal(t, int64(9000*4367*8)+studioImageMemoryOverhead, estimate)
	require.Less(t, estimate, studioImageMemoryLimit)
	budget := &studioImageMemoryBudget{}
	release, err := budget.acquire(estimate)
	require.NoError(t, err)
	_, err = budget.acquire(estimate)
	require.ErrorIs(t, err, ErrGalleryBusy, "concurrent large decodes must share process capacity")
	release()
	release()
	require.Zero(t, budget.used, "release must be idempotent")
	second, err := budget.acquire(estimate)
	require.NoError(t, err)
	second()
}

func TestStudioImageMemoryRejectsOverflowAndCapacityBeforeAllocation(t *testing.T) {
	for _, size := range [][2]int{{-1, 10}, {0, 10}, {math.MaxInt, math.MaxInt}, {50000, 50000}} {
		_, err := studioImageMemoryEstimate(size[0], size[1], 8)
		if err == nil {
			estimate, _ := studioImageMemoryEstimate(size[0], size[1], 8)
			_, err = (&studioImageMemoryBudget{}).acquire(estimate)
		}
		require.ErrorIs(t, err, ErrGalleryBusy)
	}
}

func TestStudioProcessingUsesSharedResourceBudgetInsteadOf20MPCap(t *testing.T) {
	reported := galleryPNGHeader(9000, 4367)
	memory, err := studioProcessingMemoryRequirement(reported, nil, nil, StudioPostprocessNone)
	require.NoError(t, err, "valid original dimensions must not hit an unofficial 20MP limit")
	require.Less(t, memory, studioImageMemoryLimit)
	_, _, err = ProcessStudioImage(context.Background(), galleryPNGHeader(50000, 50000), nil, nil, StudioPostprocessNone, "png", nil)
	require.ErrorIs(t, err, ErrGalleryBusy)
	_, err = studioProcessingMemoryRequirement(reported, reported, reported, StudioPostprocessMask)
	require.ErrorIs(t, err, ErrGalleryBusy, "mask operations retain multiple images and need a combined reservation")
	require.NoError(t, StudioMaskProcessingCapacity(2048, 2048))
	require.ErrorIs(t, StudioMaskProcessingCapacity(9000, 4367), ErrGalleryBusy, "an operation that can never fit must be rejected before any paid call")
	require.NoError(t, ValidateStudioMaskProcessingCapacity(2048, 2048, 2048, 2048))
	require.NoError(t, StudioMaskProcessingCapacity(4000, 4000), "the lower bound alone cannot decide whether the generated output will fit")
	require.ErrorIs(t, ValidateStudioMaskProcessingCapacity(4000, 4000, 4000, 4000), ErrGalleryBusy)
	require.ErrorIs(t, ValidateStudioMaskProcessingCapacity(2048, 2048, math.MaxInt, math.MaxInt), ErrGalleryBusy)
}
