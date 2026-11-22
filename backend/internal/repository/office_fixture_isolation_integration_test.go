//go:build integration

package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOfficeFixtureCleanupRemovesOwnedSummaryWithoutDeletingOtherUsers(t *testing.T) {
	ctx := context.Background()
	_, other := officeTestAdmission(t)
	var userID, groupID int64
	ok := t.Run("persisted summary fixture", func(t *testing.T) {
		base, input := officeTestAdmission(t)
		userID, groupID = input.UserID, input.GroupID
		input.Mode = "chat"
		chat := officeChatRepo(t, base)
		_, _, err := base.AdmitRun(ctx, input)
		require.NoError(t, err)
		officeFinishText(t, base, *officeClaimPreset(t, chat, "chat", input.RunID), "first answer")
		input = officeNextAdmission(input, "another question")
		_, _, err = base.AdmitRun(ctx, input)
		require.NoError(t, err)
		lease := officeClaimPreset(t, chat, "chat", input.RunID)
		messages, err := chat.ContextMessages(ctx, lease.Run)
		require.NoError(t, err)
		require.Len(t, messages, 3)
		hash := sha256.Sum256([]byte(messages[0].ID + "\n" + messages[1].ID))
		_, err = chat.SaveContextSummary(ctx, *lease, service.OfficeContextSummary{Content: "persisted test summary", ThroughMessageID: messages[1].ID, SourceHash: hex.EncodeToString(hash[:])}, 0, "fixture-summary")
		require.NoError(t, err)
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_agent_context_summaries WHERE user_id=$1`, userID).Scan(&count))
		require.Equal(t, 1, count, "the fixture must retain a real summary until its cleanup executes")
	})
	require.True(t, ok)
	for _, table := range []string{"office_agent_context_summaries", "office_agent_conversations", "office_agent_runs", "office_agent_messages", "api_keys"} {
		var count int
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE user_id=$1`, userID).Scan(&count))
		require.Zero(t, count, "fixture must remove its own %s before unrelated suites run", table)
	}
	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=$1`, userID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM groups WHERE id=$1`, groupID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id=$1`, other.UserID).Scan(&count))
	require.Equal(t, 1, count, "cleanup must not mask leaked fixtures by deleting another user")
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM office_agent_conversations WHERE user_id=$1`, other.UserID).Scan(&count))
	require.Equal(t, 1, count)
}
