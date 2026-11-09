package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStudioURLOriginalSurvivesExpiredLinkAndRestart(t *testing.T) {
	data := studioTestPNG(t, image.NewNRGBA(image.Rect(0, 0, 3, 2)))
	var downloads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		downloads.Add(1)
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	}))
	defer server.Close()
	response, err := json.Marshal(map[string]any{"data": []any{map[string]string{"url": server.URL + "/temporary.png"}}})
	require.NoError(t, err)
	claim := StudioJobClaim{StudioJob: StudioJob{UserID: 7, TaskID: "url-result", Spec: StudioJobSpec{Protocol: "openai"}}}
	s := &StudioJobService{spoolDir: t.TempDir()}
	uploader := NewImageResultUploader(nil, "", StudioMaxOutputImageBytes, server.Client())
	require.NoError(t, s.cacheStudioURLImages(context.Background(), claim, response, uploader))
	require.Equal(t, int32(1), downloads.Load())
	info, err := os.Stat(s.studioImageSpoolPath(claim, 0))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
	server.Close() // Simulate a supplier URL that no longer exists after restart.
	restarted := &StudioJobService{spoolDir: s.spoolDir, spoolUsed: int64(len(data))}
	require.NoError(t, restarted.cacheStudioURLImages(context.Background(), claim, response, uploader))
	items, err := studioResponseImages("openai", response)
	require.NoError(t, err)
	got, mime, err := restarted.studioImageBytes(context.Background(), claim, 0, items[0], uploader)
	require.NoError(t, err)
	require.Equal(t, data, got)
	require.Equal(t, "image/png", mime)
	require.Equal(t, int32(1), downloads.Load(), "archival retries must use the durable original")
	restarted.removeStudioResponse(restarted.studioSpoolPath(claim))
	require.Zero(t, restarted.spoolUsed)
	_, err = os.Stat(restarted.studioImageSpoolPath(claim, 0))
	require.True(t, os.IsNotExist(err))
}

func TestStudioURLSpoolResumesOnlyMissingOriginals(t *testing.T) {
	data := studioTestPNG(t, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	var first, second atomic.Int32
	var available atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/first" {
			first.Add(1)
		} else {
			second.Add(1)
			if !available.Load() {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(data)
	}))
	defer server.Close()
	response, err := json.Marshal(map[string]any{"data": []any{
		map[string]string{"url": server.URL + "/first"}, map[string]string{"url": server.URL + "/second"},
	}})
	require.NoError(t, err)
	s := &StudioJobService{spoolDir: t.TempDir()}
	claim := StudioJobClaim{StudioJob: StudioJob{UserID: 7, TaskID: "batch-url", Spec: StudioJobSpec{Protocol: "grok"}}}
	uploader := NewImageResultUploader(nil, "", StudioMaxOutputImageBytes, server.Client())
	require.Error(t, s.cacheStudioURLImages(context.Background(), claim, response, uploader))
	available.Store(true)
	require.NoError(t, s.cacheStudioURLImages(context.Background(), claim, response, uploader))
	require.Equal(t, int32(1), first.Load())
	require.Equal(t, int32(2), second.Load())
	require.Equal(t, int64(2*len(data)), s.spoolUsed)
}

func TestStudioBase64OriginalDoesNotNeedDuplicateSidecar(t *testing.T) {
	data := studioTestPNG(t, image.NewNRGBA(image.Rect(0, 0, 2, 2)))
	response, err := json.Marshal(map[string]any{"data": []any{map[string]string{"b64_json": base64.StdEncoding.EncodeToString(data)}}})
	require.NoError(t, err)
	s := &StudioJobService{spoolDir: t.TempDir()}
	claim := StudioJobClaim{StudioJob: StudioJob{UserID: 7, TaskID: "inline-result", Spec: StudioJobSpec{Protocol: "openai"}}}
	require.NoError(t, s.cacheStudioURLImages(context.Background(), claim, response, nil))
	files, err := os.ReadDir(s.spoolDir)
	require.NoError(t, err)
	require.Empty(t, files)
}

func TestStudioURLOriginalSpoolRespectsCapacity(t *testing.T) {
	dir := t.TempDir()
	s := &StudioJobService{spoolDir: dir, spoolUsed: studioJobMaxSpoolBytes}
	path := filepath.Join(dir, "u7__limited__0.json.image-0")
	require.Error(t, s.writeStudioImageSpool(path, 1, []byte{1}))
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, files)
	require.Equal(t, studioJobMaxSpoolBytes, s.spoolUsed)
}

func TestStudioSpoolReservesURLBatchBeforePaidDispatch(t *testing.T) {
	claim := StudioJobClaim{StudioJob: StudioJob{UserID: 7, TaskID: "ten-images", Spec: StudioJobSpec{Protocol: "grok", ImageCount: 10}}}
	s := &StudioJobService{spoolDir: t.TempDir(), spoolUsed: studioJobMaxSpoolBytes - studioJobMaxResponseBytes}
	require.Error(t, s.reserveStudioSpool(claim), "a small URL response must not hide the later original-image capacity")
	require.Zero(t, s.spoolReserved)
	s.spoolUsed = 0
	require.Equal(t, int64(512<<20), studioJobMaxTaskSpoolBytes)
	require.Greater(t, GalleryMaxImageBytes, StudioMaxOutputImageBytes, "reference limits must not enlarge paid output reservations")
	require.LessOrEqual(t, studioSpoolReservationBytes(claim), studioJobMaxTaskSpoolBytes, "the original ten-image native batch must remain supported")
	require.NoError(t, s.reserveStudioSpool(claim))
	require.Equal(t, studioJobMaxResponseBytes+10*int64(StudioMaxOutputImageBytes), s.spoolReserved)
	s.releaseStudioReservation(claim)
	require.Zero(t, s.spoolReserved)
}

func TestStudioGeneratedResultsKeepSeparateOutputBound(t *testing.T) {
	data := make([]byte, StudioMaxOutputImageBytes+1)
	s := &ImageStudioGalleryService{}
	require.ErrorIs(t, s.PutAcceptedJobAsset(context.Background(), 7, "job", "output", "image/png", data), ErrGalleryInvalid)
	_, _, err := boundedStudioOutput(data, "image/png")
	require.ErrorIs(t, err, errStudioImageInvalid)
	_, _, err = ProcessStudioImage(context.Background(), data, nil, nil, StudioPostprocessNone, "png", nil)
	require.ErrorIs(t, err, errStudioImageInvalid)
}
