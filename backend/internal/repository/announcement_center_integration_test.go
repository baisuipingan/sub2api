//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementCenterMigrationPaginationAndSnapshotReadLifecycle(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewAnnouncementRepository(integrationEntClient)
	reads := NewAnnouncementReadRepository(integrationEntClient)
	svc := service.NewAnnouncementService(repo, reads, NewUserRepository(integrationEntClient, integrationDB), NewUserSubscriptionRepository(integrationEntClient))
	svc.SetSnapshotKey("integration-shared-secret")
	before, err := svc.CenterSummary(ctx, userID)
	require.NoError(t, err)
	stamp := time.Now().Add(-time.Hour).Truncate(time.Microsecond)
	ids := make([]int64, 0, 276)
	t.Cleanup(func() {
		for _, id := range ids {
			_, _ = integrationDB.ExecContext(ctx, `DELETE FROM announcements WHERE id=$1`, id)
		}
	})
	for i := 0; i < 275; i++ {
		a := &service.Announcement{Title: fmt.Sprintf("center-%d", i), Content: "**full Markdown**", Status: service.AnnouncementStatusActive, NotifyMode: service.AnnouncementNotifyModeSilent, DisplayMode: service.AnnouncementDisplayTimeline}
		require.NoError(t, repo.Create(ctx, a))
		ids = append(ids, a.ID)
		_, err = integrationDB.ExecContext(ctx, `UPDATE announcements SET created_at=$2,updated_at=$2 WHERE id=$1`, a.ID, stamp.Add(time.Duration(i)*time.Second))
		require.NoError(t, err)
	}
	notice := &service.Announcement{Title: "fixed notice", Content: "complete instructions", Status: service.AnnouncementStatusActive, DisplayMode: service.AnnouncementDisplayNotice, NotifyMode: service.AnnouncementNotifyModePopup}
	require.NoError(t, repo.Create(ctx, notice))
	ids = append(ids, notice.ID)
	metadata, err := repo.(service.AnnouncementCenterRepository).ListCenterCandidates(ctx, time.Now())
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(metadata), 276)
	for _, a := range metadata {
		require.Empty(t, a.Content, "count/list metadata scan must not read every Markdown body")
	}
	summary, err := svc.CenterSummary(ctx, userID)
	require.NoError(t, err)
	require.Equal(t, before.TimelineCount+275, summary.TimelineCount)
	require.Equal(t, before.TimelineUnreadCount+275, summary.TimelineUnreadCount)
	require.Equal(t, before.NoticeCount+1, summary.NoticeCount)
	items, page, err := svc.ListCenterForUser(ctx, userID, service.AnnouncementDisplayTimeline, pagination.PaginationParams{Page: 14, PageSize: 20})
	require.NoError(t, err)
	require.GreaterOrEqual(t, page.Total, int64(275))
	require.NotEmpty(t, items)
	// Revisions changed after the center opened must remain unread. Moving an
	// item to notice is also excluded, without deleting its read history.
	_, err = integrationDB.ExecContext(ctx, `UPDATE announcements SET content='important edit',updated_at=NOW() WHERE id=$1`, ids[0])
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE announcements SET display_mode='notice',updated_at=NOW() WHERE id=$1`, ids[1])
	require.NoError(t, err)
	late := &service.Announcement{Title: "later", Content: "new", Status: service.AnnouncementStatusActive, DisplayMode: service.AnnouncementDisplayTimeline}
	require.NoError(t, repo.Create(ctx, late))
	ids = append(ids, late.ID)
	firstRead := time.Now().Add(-time.Minute).Truncate(time.Microsecond)
	require.NoError(t, reads.MarkRead(ctx, ids[2], userID, firstRead))
	result, err := svc.MarkAllTimelineRead(ctx, userID, summary.Snapshot)
	require.NoError(t, err)
	require.NotContains(t, result.MarkedIDs, ids[0])
	require.NotContains(t, result.MarkedIDs, ids[1])
	require.NotContains(t, result.MarkedIDs, late.ID)
	require.NotContains(t, result.MarkedIDs, notice.ID)
	actual, err := reads.GetReadMapByUser(ctx, userID, ids)
	require.NoError(t, err)
	require.True(t, firstRead.Equal(actual[ids[2]]))
	require.NotContains(t, actual, ids[0])
	require.NotContains(t, actual, late.ID)
	require.Contains(t, actual, ids[274])
	var defaultMode string
	var oldID int64
	require.NoError(t, integrationDB.QueryRowContext(ctx, `INSERT INTO announcements(title,content) VALUES('legacy-default','content') RETURNING id,display_mode`).Scan(&oldID, &defaultMode))
	ids = append(ids, oldID)
	require.Equal(t, "timeline", defaultMode)
	_, err = integrationDB.ExecContext(ctx, `UPDATE announcements SET display_mode='both' WHERE id=$1`, oldID)
	require.Error(t, err)
}

func TestAnnouncementBatchReadRechecksRevisionAndPreservesFirstReadInSQL(t *testing.T) {
	ctx := context.Background()
	userID := galleryTestUser(t)
	repo := NewAnnouncementRepository(integrationEntClient)
	reads := NewAnnouncementReadRepository(integrationEntClient)
	ann := &service.Announcement{Title: "revision", Content: "body", Status: service.AnnouncementStatusActive, DisplayMode: service.AnnouncementDisplayTimeline}
	require.NoError(t, repo.Create(ctx, ann))
	t.Cleanup(func() { _, _ = integrationDB.ExecContext(ctx, `DELETE FROM announcements WHERE id=$1`, ann.ID) })
	current, err := repo.GetByID(ctx, ann.ID)
	require.NoError(t, err)
	revs := []service.AnnouncementReadRevision{{ID: ann.ID, UpdatedAt: current.UpdatedAt}}
	_, err = integrationDB.ExecContext(ctx, `UPDATE announcements SET updated_at=updated_at+INTERVAL '1 second' WHERE id=$1`, ann.ID)
	require.NoError(t, err)
	marked, err := reads.(service.AnnouncementCenterReadRepository).MarkReadRevisions(ctx, userID, revs, time.Now())
	require.NoError(t, err)
	require.Empty(t, marked)
	current, err = repo.GetByID(ctx, ann.ID)
	require.NoError(t, err)
	revs[0].UpdatedAt = current.UpdatedAt
	first := time.Now().Truncate(time.Microsecond)
	marked, err = reads.(service.AnnouncementCenterReadRepository).MarkReadRevisions(ctx, userID, revs, first)
	require.NoError(t, err)
	require.Equal(t, []int64{ann.ID}, marked)
	marked, err = reads.(service.AnnouncementCenterReadRepository).MarkReadRevisions(ctx, userID, revs, time.Now())
	require.NoError(t, err)
	require.Empty(t, marked)
	got, err := reads.GetReadMapByUser(ctx, userID, []int64{ann.ID})
	require.NoError(t, err)
	require.True(t, first.Equal(got[ann.ID]))
}
