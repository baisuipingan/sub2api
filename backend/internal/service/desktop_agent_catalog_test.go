//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type desktopCatalogRepository struct{ *desktopAgentSettingsStub }

func (r *desktopCatalogRepository) Set(ctx context.Context, key, value string) error {
	return r.SetMultiple(ctx, map[string]string{key: value})
}

type desktopCatalogReadCountingRepository struct {
	*desktopAgentSettingsStub
	reads  int
	onRead func()
}

func (r *desktopCatalogReadCountingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	r.reads++
	if r.onRead != nil {
		r.onRead()
	}
	return r.desktopAgentSettingsStub.GetMultiple(ctx, keys)
}

type desktopCatalogTransport func(*http.Request) (*http.Response, error)

func (f desktopCatalogTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func newDesktopCatalogFixture(t *testing.T) (*DesktopAgentProfileCatalogService, *desktopCatalogRepository, *string) {
	t.Helper()
	repo := &desktopCatalogRepository{&desktopAgentSettingsStub{values: map[string]string{}}}
	svc := NewDesktopAgentProfileCatalogService(nil, NewSettingService(repo, nil))
	t.Cleanup(svc.Stop)
	body := ""
	svc.client = &http.Client{Transport: desktopCatalogTransport(func(request *http.Request) (*http.Response, error) {
		require.Empty(t, request.Header.Get("Authorization"))
		require.Empty(t, request.Header.Get("x-api-key"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	return svc, repo, &body
}
func catalogDocumentFixture(t *testing.T, profiles []DesktopAgentProfile) string {
	t.Helper()
	body, err := json.Marshal(DesktopAgentProfileCatalogDocument{SchemaVersion: 1, Revision: "2026-11-14.2", Profiles: profiles})
	require.NoError(t, err)
	return string(body)
}
func newRemoteProfileFixture() DesktopAgentProfile {
	p := desktopAgentOpenAIProfiles()[0]
	p.ID = "remote-catalog-model"
	p.Model.ID = p.ID
	p.Aliases = []string{p.ID, "remote-official-alias"}
	return p
}
func TestDesktopAgentRemoteCatalogBuiltinSchemaAndUnknownOutput(t *testing.T) {
	require.NoError(t, validateDesktopAgentCatalogProfiles(desktopAgentBuiltinProfiles()))
	raw := catalogDocumentFixture(t, desktopAgentBuiltinProfiles())
	var parsed DesktopAgentProfileCatalogDocument
	require.NoError(t, decodeDesktopJSON([]byte(raw), &parsed))
	require.Len(t, parsed.Profiles, 37)
}
func TestDesktopAgentRemoteCatalogPersistsAndRetainsOmissionsAcrossRestart(t *testing.T) {
	svc, repo, body := newDesktopCatalogFixture(t)
	remote := newRemoteProfileFixture()
	*body = catalogDocumentFixture(t, []DesktopAgentProfile{remote})
	require.NoError(t, svc.Refresh(context.Background()))
	require.Equal(t, 38, svc.Status().ProfileCount)
	require.False(t, svc.Status().UsingFallback)
	p := svc.Snapshot()
	p[0].Model.ContextWindow = 1
	require.NotEqual(t, 1, svc.Snapshot()[0].Model.ContextWindow)
	*body = catalogDocumentFixture(t, desktopAgentOpenAIProfiles())
	require.NoError(t, svc.Refresh(context.Background()))
	reloaded := NewDesktopAgentProfileCatalogService(nil, NewSettingService(repo, nil))
	t.Cleanup(reloaded.Stop)
	require.NoError(t, reloaded.Initialize(context.Background()))
	_, profile := desktopAgentProfileBinding(DesktopAgentConfig{}, remote.ID, reloaded.Snapshot())
	require.NotNil(t, profile)
	require.Contains(t, profile.Aliases, "remote-official-alias")
	require.Equal(t, "2026-11-14.2", reloaded.Status().Revision)
}
func TestDesktopAgentRemoteCatalogRejectsWholeInvalidUpdatesAndKeepsLastGood(t *testing.T) {
	svc, repo, body := newDesktopCatalogFixture(t)
	*body = catalogDocumentFixture(t, []DesktopAgentProfile{newRemoteProfileFixture()})
	require.NoError(t, svc.Refresh(context.Background()))
	before := svc.Snapshot()
	saved := repo.values[SettingKeyDesktopAgentProfileCatalog]
	cases := map[string]string{"empty": "{\"schema_version\":1,\"revision\":\"bad\",\"profiles\":[]}", "duplicate": "{\"schema_version\":1,\"schema_version\":1,\"revision\":\"bad\",\"profiles\":[]}", "unknown-field": strings.Replace(*body, "\"schema_version\":1", "\"schema_version\":1,\"credentials\":{}", 1), "large": strings.Repeat("x", desktopAgentCatalogMaxBytes+1)}
	duplicate := newRemoteProfileFixture()
	duplicate.Aliases = append(duplicate.Aliases, "gpt-6.1-sol")
	cases["alias-hijack"] = catalogDocumentFixture(t, []DesktopAgentProfile{duplicate})
	wrong := newRemoteProfileFixture()
	wrong.SourceURL = "https://example.test/official"
	cases["fake-official"] = catalogDocumentFixture(t, []DesktopAgentProfile{wrong})
	wrong = newRemoteProfileFixture()
	wrong.Model.ContextWindow--
	cases["derived-context"] = catalogDocumentFixture(t, []DesktopAgentProfile{wrong})
	wrong = newRemoteProfileFixture()
	wrong.Model.SupportsToolCall = nil
	cases["null-capability"] = catalogDocumentFixture(t, []DesktopAgentProfile{wrong})
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			*body = raw
			require.Error(t, svc.Refresh(context.Background()))
			require.Equal(t, before, svc.Snapshot())
			require.Equal(t, saved, repo.values[SettingKeyDesktopAgentProfileCatalog])
			require.NotEmpty(t, svc.Status().LastError)
		})
	}
	svc.client = &http.Client{Transport: desktopCatalogTransport(func(*http.Request) (*http.Response, error) { return nil, errors.New("secret-external-url-password") })}
	require.Error(t, svc.Refresh(context.Background()))
	require.NotContains(t, svc.Status().LastError, "secret")
	repo.err = errors.New("database-failed")
	*body = catalogDocumentFixture(t, []DesktopAgentProfile{newRemoteProfileFixture()})
}
func TestDesktopAgentRemoteCatalogSourceSafetyAndIndependentDraft(t *testing.T) {
	svc, repo, _ := newDesktopCatalogFixture(t)
	realClient := NewDesktopAgentProfileCatalogService(nil, nil)
	t.Cleanup(realClient.Stop)
	require.NotNil(t, realClient.client.CheckRedirect)
	for _, source := range []string{"http://example.com/file", "https://localhost/file", "https://127.0.0.1/file", "https://user:secret@example.com/file", "https://example.com/file?secret=value", "https://example.com/file#fragment"} {
		require.Error(t, svc.SetSource(context.Background(), source), source)
	}
	require.Empty(t, repo.written)
	require.NoError(t, svc.SetSource(context.Background(), ""))
	require.Empty(t, svc.Status().SourceURL)
	require.NoError(t, svc.Refresh(context.Background()))
	require.NotContains(t, repo.written, SettingKeyDesktopAgentConfig)
}
func TestDesktopAgentRemoteCatalogCurrentBindingConflictBlocksRefresh(t *testing.T) {
	service, settings, _, _, keys := desktopFixture(t)
	svc, repo, body := newDesktopCatalogFixture(t)
	// This custom name already belongs to a different explicit profile.
	cfg := validDesktopConfig()
	cfg.Model.ID = "remote-catalog-model"
	cfg.ModelProfiles = desktopTestProfileBindings(cfg.Model.ID)
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	settings.values[SettingKeyDesktopAgentConfig] = string(raw)
	svc.repo = repo
	svc.validate = service.preflightProfileCatalog
	*body = catalogDocumentFixture(t, []DesktopAgentProfile{newRemoteProfileFixture()})
	require.Error(t, svc.Refresh(context.Background()))
	require.Equal(t, "configuration_conflict", svc.Status().LastError)
	require.Zero(t, keys.calls)
	require.Empty(t, repo.written)
}
func TestDesktopAgentRemoteCatalogUsedByAdminParseAndAllBootstrapReads(t *testing.T) {
	svc, repo, _, _, keys := desktopFixture(t)
	catalog, _, _ := newDesktopCatalogFixture(t)
	remote := newRemoteProfileFixture()
	catalog.profiles = append(desktopAgentBuiltinProfiles(), remote)
	svc.profileCatalog = catalog
	cfg := validDesktopConfig()
	cfg.Model = nil
	cfg.Models = []DesktopAgentModel{remote.Model}
	cfg.DefaultModelID = remote.ID
	cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{remote.ID: {ProfileID: remote.ID}}
	svc.catalog = &desktopCatalogStub{models: []string{remote.ID}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	_, err = svc.UpdateConfigJSON(context.Background(), raw)
	require.NoError(t, err)
	stored := repo.values[SettingKeyDesktopAgentConfig]
	keys.onEnsure = func() { catalog.mu.Lock(); catalog.profiles = desktopAgentBuiltinProfiles(); catalog.mu.Unlock() }
	result, err := svc.BootstrapForProtocol(context.Background(), 7, 4)
	require.NoError(t, err)
	require.Equal(t, "ready", result.ServiceStatus)
	require.Equal(t, remote.ID, result.Models[0].ID)
	require.Equal(t, stored, repo.values[SettingKeyDesktopAgentConfig])
	// Future reads use the new snapshot and fail safely; the in-flight read above
	// stayed on the snapshot that validated its initial config.
	_, err = svc.GetConfig(context.Background())
	require.Error(t, err)
}
func TestDesktopAgentProtocolFourTruthfulUnknownAndLegacyIsolation(t *testing.T) {
	svc, repo, _, _, keys := desktopFixture(t)
	good := desktopAgentOpenAIProfiles()[0].Model
	unknown := desktopAgentGrokProfiles()[0].Model
	equal := desktopAgentGeminiProfiles()[0].Model
	require.Zero(t, unknown.MaxOutputTokens)
	require.Equal(t, equal.ContextWindow, equal.MaxOutputTokens)
	cfg := validDesktopConfig()
	cfg.Model = nil
	cfg.ModelProfiles = nil
	cfg.Models = []DesktopAgentModel{good, unknown, equal}
	cfg.DefaultModelID = unknown.ID
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo.values[SettingKeyDesktopAgentConfig] = string(raw)
	svc.catalog = &desktopCatalogStub{models: []string{good.ID, unknown.ID, equal.ID}}
	for _, protocol := range []int{1, 2} {
		result, err := svc.BootstrapForProtocol(context.Background(), 7, protocol)
		require.NoError(t, err)
		require.Equal(t, "not_configured", result.ServiceStatus)
		require.Nil(t, result.Credential)
	}
	require.Zero(t, keys.calls)
	result, err := svc.BootstrapForProtocol(context.Background(), 7, 3)
	require.NoError(t, err)
	require.Equal(t, "ready", result.ServiceStatus)
	require.Len(t, result.Models, 1)
	require.Len(t, result.UnavailableModels, 2)
	require.Nil(t, result.SelectionPolicyV3.Default)
	result, err = svc.BootstrapForProtocol(context.Background(), 7, 4)
	require.NoError(t, err)
	require.Equal(t, "ready", result.ServiceStatus)
	require.Len(t, result.Models, 3)
	require.Equal(t, unknown.ID, result.SelectionPolicyV3.Default.ModelID)
	body, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(body), `"output_limit_kind":"unlimited"`)
	require.NotContains(t, string(body), "official_max_output_tokens")
	var wire map[string]any
	require.NoError(t, json.Unmarshal(body, &wire))
	require.Equal(t, float64(4), wire["schema_version"])
	require.NotEmpty(t, wire["credential"].(map[string]any)["authorization_scope"])
}

func TestDesktopAgentRemoteCatalogRejectsRedirectAndCancelsOnStop(t *testing.T) {
	svc, _, _ := newDesktopCatalogFixture(t)
	realClient := NewDesktopAgentProfileCatalogService(nil, nil)
	t.Cleanup(realClient.Stop)
	calls := 0
	realClient.repo = svc.repo
	realClient.client.Transport = desktopCatalogTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusFound, Header: http.Header{"Location": []string{"https://127.0.0.1/secret"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	require.Error(t, realClient.Refresh(context.Background()))
	require.Equal(t, 1, calls)
	started := make(chan struct{})
	finished := make(chan error, 1)
	svc.client = &http.Client{Transport: desktopCatalogTransport(func(r *http.Request) (*http.Response, error) {
		close(started)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})}
	go func() { finished <- svc.Refresh(context.Background()) }()
	<-started
	svc.Stop()
	select {
	case err := <-finished:
		require.Error(t, err)
	case <-time.After(time.Second):
		t.Fatal("catalog stop did not cancel in-flight download")
	}
}

func TestDesktopAgentRemoteCatalogRetainsCacheAndAllowsSourceRepairAfterAllowlistTightening(t *testing.T) {
	svc, repo, _ := newDesktopCatalogFixture(t)
	require.NoError(t, svc.SetSource(context.Background(), "https://old-download.example.com/catalog.json"))
	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = true
	cfg.Security.URLAllowlist.PricingHosts = []string{"raw.githubusercontent.com"}
	reloaded := NewDesktopAgentProfileCatalogService(cfg, NewSettingService(repo, nil))
	t.Cleanup(reloaded.Stop)
	require.NoError(t, reloaded.Initialize(context.Background()))
	require.Equal(t, 37, len(reloaded.Snapshot()))
	require.Error(t, reloaded.Refresh(context.Background()))
	require.Equal(t, "source_invalid", reloaded.Status().LastError)
	require.NoError(t, reloaded.SetSource(context.Background(), DefaultDesktopAgentProfileCatalogURL))
	require.Equal(t, DefaultDesktopAgentProfileCatalogURL, reloaded.Status().SourceURL)
}

func TestDesktopAgentRemoteCatalogPersistenceDoesNotPublishPartialSnapshot(t *testing.T) {
	svc, repo, body := newDesktopCatalogFixture(t)
	*body = catalogDocumentFixture(t, []DesktopAgentProfile{newRemoteProfileFixture()})
	require.NoError(t, svc.Refresh(context.Background()))
	beforeProfiles, beforeRaw := svc.requestSnapshot()
	beforeStatus := svc.Status()
	candidate := cloneDesktopAgentProfiles(beforeProfiles)
	candidate[0].DisplayName += " updated"
	record := desktopAgentProfileCatalogRecord{SchemaVersion: 1, SourceURL: beforeStatus.SourceURL, Revision: "next", Profiles: candidate, LastSuccessAt: time.Now().UTC().Format(time.RFC3339)}
	repo.err = errors.New("database unavailable")
	failedRaw, err := svc.persist(context.Background(), beforeRaw, record, nil)
	require.Error(t, err)
	require.Empty(t, failedRaw)
	actualProfiles, actualRaw := svc.requestSnapshot()
	require.Equal(t, beforeProfiles, actualProfiles)
	require.Equal(t, beforeRaw, actualRaw)
	require.Equal(t, beforeStatus, svc.Status())
	repo.err = nil
	acceptedRaw, err := svc.persist(context.Background(), beforeRaw, record, nil)
	require.NoError(t, err)
	require.Equal(t, acceptedRaw, repo.values[SettingKeyDesktopAgentProfileCatalog])
	// A committed database record must not expose its CAS guard with the old
	// in-memory capabilities while publication is still pending.
	actualProfiles, actualRaw = svc.requestSnapshot()
	require.Equal(t, beforeProfiles, actualProfiles)
	require.Equal(t, beforeRaw, actualRaw)
	require.Equal(t, beforeStatus, svc.Status())
	svc.publish(record, acceptedRaw)
	actualProfiles, actualRaw = svc.requestSnapshot()
	require.Equal(t, candidate, actualProfiles)
	require.Equal(t, acceptedRaw, actualRaw)
	require.Equal(t, "next", svc.Status().Revision)
}

func TestDesktopAgentRemoteCatalogConcurrentSnapshotsStayOnOnePublishedRevision(t *testing.T) {
	svc, _, body := newDesktopCatalogFixture(t)
	*body = catalogDocumentFixture(t, []DesktopAgentProfile{newRemoteProfileFixture()})
	require.NoError(t, svc.Refresh(context.Background()))
	stop := make(chan struct{})
	failures := make(chan error, 4)
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				profiles, raw := svc.requestSnapshot()
				var record desktopAgentProfileCatalogRecord
				if err := json.Unmarshal([]byte(raw), &record); err != nil {
					failures <- err
					return
				}
				if !reflect.DeepEqual(record.Profiles, profiles) {
					failures <- fmt.Errorf("capability snapshot and CAS guard belong to different revisions")
					return
				}
				// Status belongs to that same publication, independently of
				// LastCheckedAt/LastError metadata updated while fetching.
				svc.mu.RLock()
				status, raw := svc.status, svc.persistedRaw
				svc.mu.RUnlock()
				if err := json.Unmarshal([]byte(raw), &record); err != nil {
					failures <- err
					return
				}
				if status.Revision != record.Revision || status.SourceURL != record.SourceURL || status.ProfileCount != len(record.Profiles) {
					failures <- fmt.Errorf("catalog status and CAS guard belong to different revisions")
					return
				}
			}
		}()
	}
	defer func() {
		close(stop)
		readers.Wait()
		close(failures)
		for err := range failures {
			require.NoError(t, err)
		}
	}()
	for iteration := range 24 {
		profile := newRemoteProfileFixture()
		profile.DisplayName = fmt.Sprintf("Published revision %d", iteration)
		*body = catalogDocumentFixture(t, []DesktopAgentProfile{profile})
		require.NoError(t, svc.Refresh(context.Background()))
		source := DefaultDesktopAgentProfileCatalogURL
		if iteration%2 == 0 {
			source = "https://raw.githubusercontent.com/baisuipingan/sub2api/model-profiles/catalog.json"
		}
		require.NoError(t, svc.SetSource(context.Background(), source))
	}
}

func TestDesktopAgentRemoteCatalogPreflightReadsConfigOnceAndFreezesPreviousProfiles(t *testing.T) {
	svc, repo, _, groups, keys := desktopFixture(t)
	catalog, _, _ := newDesktopCatalogFixture(t)
	oldProfile := newRemoteProfileFixture()
	oldProfile.Model.ReasoningLevels = []string{"low"}
	oldProfile.Model.DefaultReasoning = "low"
	catalog.profiles = append(desktopAgentBuiltinProfiles(), oldProfile)
	svc.profileCatalog = catalog
	groups.group.MaxReasoningEffort = "low"
	svc.catalog = &desktopCatalogStub{models: []string{oldProfile.ID}}
	cfg := validDesktopConfig()
	cfg.Model = nil
	cfg.Models = []DesktopAgentModel{oldProfile.Model}
	cfg.DefaultModelID = oldProfile.ID
	cfg.ModelProfiles = map[string]DesktopAgentModelProfileBinding{oldProfile.ID: {ProfileID: oldProfile.ID}}
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	repo.values[SettingKeyDesktopAgentConfig] = string(raw)
	candidate := cloneDesktopAgentProfiles(catalog.profiles)
	candidate[len(candidate)-1].Model.ReasoningLevels = []string{"low", "high"}
	counted := &desktopCatalogReadCountingRepository{desktopAgentSettingsStub: repo}
	counted.onRead = func() {
		// Simulate another accepted catalog arriving during the config read.
		// The before-policy must still use the snapshot taken at entry.
		catalog.mu.Lock()
		catalog.profiles = candidate
		catalog.mu.Unlock()
	}
	svc.settings = NewSettingService(counted, nil)
	guard, err := svc.preflightProfileCatalog(context.Background(), candidate)
	require.ErrorIs(t, err, ErrDesktopAgentInvalid)
	require.Equal(t, string(raw), guard)
	require.Equal(t, 1, counted.reads)
	require.Zero(t, keys.calls)
}
