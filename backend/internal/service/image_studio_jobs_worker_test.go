package service

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStudioSpoolOwnerPersistsAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	first, err := studioSpoolOwner(dir)
	require.NoError(t, err)
	second, err := studioSpoolOwner(dir)
	require.NoError(t, err)
	require.Equal(t, first, second)
	info, err := os.Stat(filepath.Join(dir, ".owner"))
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestStudioSpoolWriterRejectsOversizeWithoutUnboundedWrite(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "response")
	require.NoError(t, err)
	defer func() { _ = file.Close() }()
	writer := &studioSpoolWriter{file: file, limit: 4}
	n, err := writer.Write([]byte("1234"))
	require.NoError(t, err)
	require.Equal(t, 4, n)
	n, err = writer.Write([]byte("5"))
	require.Error(t, err)
	require.Zero(t, n)
	require.Equal(t, int64(4), writer.written)
	require.NoError(t, file.Sync())
	data, err := os.ReadFile(file.Name())
	require.NoError(t, err)
	require.True(t, bytes.Equal([]byte("1234"), data))
}

func TestStudioOutputIDIsStableAndWithinGalleryLimit(t *testing.T) {
	claim := StudioJobClaim{StudioJob: StudioJob{UserID: 7, TaskID: strings.Repeat("x", 128)}, Call: StudioJobCall{Ordinal: 9}}
	id := studioOutputID(claim, 9, "out")
	require.True(t, galleryIDValid(id))
	require.Equal(t, id, studioOutputID(claim, 9, "out"))
	require.NotEqual(t, id, studioOutputID(claim, 9, "raw"))
	require.NotEqual(t, id, studioOutputID(claim, 8, "out"))
	require.LessOrEqual(t, len(studioClientRequestID(claim)), 64)
	otherCall := claim
	otherCall.Call.Ordinal = 8
	require.NotEqual(t, studioClientRequestID(claim), studioClientRequestID(otherCall))
}

func TestStudioImageMultipartPreservesMIME(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := studioCreateImagePart(writer, "image[]", "reference-1.png", "image/png")
	require.NoError(t, err)
	_, err = part.Write([]byte("PNG"))
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	reader := multipart.NewReader(&body, writer.Boundary())
	got, err := reader.NextPart()
	require.NoError(t, err)
	require.Equal(t, "image/png", got.Header.Get("Content-Type"))
	require.Equal(t, "image[]", got.FormName())
}

func TestStudioStopKeepsRouterUntilClaimFinishes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	s := &StudioJobService{router: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }), cancel: cancel}
	s.workers.Add(1)
	release := make(chan struct{})
	workerResult := make(chan bool, 1)
	go func() {
		defer s.workers.Done()
		<-ctx.Done()
		<-release
		s.mu.Lock()
		router := s.router
		s.mu.Unlock()
		if router == nil {
			workerResult <- false
			return
		}
		writer := httptest.NewRecorder()
		router.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, "/", nil))
		workerResult <- writer.Code == http.StatusNoContent
	}()
	stopped := make(chan struct{})
	go func() { s.Stop(); close(stopped) }()
	<-ctx.Done()
	s.mu.Lock()
	require.NotNil(t, s.router)
	s.mu.Unlock()
	close(release)
	require.True(t, <-workerResult)
	<-stopped
	s.mu.Lock()
	require.Nil(t, s.router)
	s.mu.Unlock()
}

func TestStudioRouteSnapshotRejectsChangedProviderOrTransport(t *testing.T) {
	spec := StudioJobSpec{Protocol: "openai", Model: "public-image"}
	known := ImageStudioModelProfile{Provider: "openai", Profile: "gpt-image-2"}
	models := []ImageStudioModel{{ID: "public-image", API: "openai", Profiles: []ImageStudioModelProfile{known}}}
	require.True(t, studioProfilesCompatible(studioCurrentProfiles(spec, models), []ImageStudioModelProfile{known}))
	models[0].Profiles = append(models[0].Profiles, ImageStudioModelProfile{Provider: "openai", Profile: "gpt-image-2", Transport: "bridge"})
	require.False(t, studioProfilesCompatible(studioCurrentProfiles(spec, models), []ImageStudioModelProfile{known}))
	models[0].Profiles = []ImageStudioModelProfile{{Provider: "xai", Profile: "grok-imagine-image"}}
	require.Empty(t, studioCurrentProfiles(spec, models))
	googleCompat := ImageStudioModelProfile{Provider: "google", Profile: "gemini-3-pro-image"}
	models[0].Profiles = []ImageStudioModelProfile{googleCompat}
	require.Equal(t, []ImageStudioModelProfile{googleCompat}, studioCurrentProfiles(spec, models))
	unknown := ImageStudioModelProfile{Provider: "unknown", Profile: "unknown"}
	models[0].Profiles = []ImageStudioModelProfile{unknown}
	require.True(t, studioProfilesCompatible(studioCurrentProfiles(spec, models), []ImageStudioModelProfile{unknown}))
	require.False(t, studioProfilesCompatible(studioCurrentProfiles(spec, models), []ImageStudioModelProfile{known}))
}
