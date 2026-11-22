package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type officeManifestTestRepo struct {
	*officeBridgeTestRepo
	OfficeChatRepository
}

func (r *officeManifestTestRepo) ClaimPreset(context.Context, string, string, time.Duration) (*OfficeRunLease, error) {
	value := r.lease
	return &value, nil
}
func (r *officeManifestTestRepo) ContextMessages(context.Context, OfficeRun) ([]OfficeMessage, error) {
	return []OfficeMessage{{ID: uuid.NewString(), RunID: r.lease.Run.ID, Role: "user", Content: json.RawMessage(`{"text":"办公问题"}`)}}, nil
}
func (r *officeManifestTestRepo) Checkpoint(context.Context, OfficeRunLease) (json.RawMessage, int64, error) {
	return nil, 0, nil
}
func (r *officeManifestTestRepo) ContextSummary(context.Context, OfficeRunLease) (*OfficeContextSummary, error) {
	return nil, nil
}

func TestOfficeClaimWireContractUsesEmptyArraysAndObjectsForPython(t *testing.T) {
	for _, mode := range []string{"chat", "office"} {
		t.Run(mode, func(t *testing.T) {
			svc, base := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
			base.lease.Run.Mode = mode
			base.lease.Run.Parameters = nil
			base.lease.Run.InputResources = nil
			svc.repo = &officeManifestTestRepo{officeBridgeTestRepo: base}
			manifest, err := svc.ClaimPreset(context.Background(), "contract-worker", mode)
			require.NoError(t, err)
			encoded, err := json.Marshal(manifest)
			require.NoError(t, err)
			var fields map[string]json.RawMessage
			require.NoError(t, json.Unmarshal(encoded, &fields))
			require.JSONEq(t, `[]`, string(fields["input_resources"]))
			require.JSONEq(t, `{}`, string(fields["parameters"]))
			require.JSONEq(t, `{"version":0,"content":"","through_message_id":"","source_hash":""}`, string(fields["context_summary"]))
			require.Len(t, manifest.Messages, 1)
			require.NotEmpty(t, manifest.CurrentQuestionID)
			require.Equal(t, mode, manifest.Mode)
			require.NotContains(t, string(encoded), "supports_vision\":null")
		})
	}
}

func TestOfficeExpiredLeaseGraceIsLimitedToReportingSuspension(t *testing.T) {
	svc, repo := officeBridgeFixture(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	credential := officeLeaseCredential{repo.lease.Run.ID, repo.lease.Run.UserID, repo.lease.Owner, repo.lease.Token, repo.lease.Epoch, time.Now().Add(-30 * time.Second).Unix()}
	raw, _ := json.Marshal(credential)
	value := base64.RawURLEncoding.EncodeToString(raw)
	token := value + "." + svc.credentialSignature(value)
	_, err := svc.AuthenticateLease(context.Background(), repo.lease.Run.ID, strconv.FormatInt(repo.lease.Epoch, 10), token)
	require.ErrorIs(t, err, ErrOfficeUnauthorized)
	lease, err := svc.AuthenticateSuspensionLease(context.Background(), repo.lease.Run.ID, strconv.FormatInt(repo.lease.Epoch, 10), token)
	require.NoError(t, err)
	require.Equal(t, repo.lease.Token, lease.Token)
	credential.Expiry = time.Now().Add(-6 * time.Minute).Unix()
	raw, _ = json.Marshal(credential)
	value = base64.RawURLEncoding.EncodeToString(raw)
	_, err = svc.AuthenticateSuspensionLease(context.Background(), repo.lease.Run.ID, strconv.FormatInt(repo.lease.Epoch, 10), value+"."+svc.credentialSignature(value))
	require.ErrorIs(t, err, ErrOfficeUnauthorized)
}
