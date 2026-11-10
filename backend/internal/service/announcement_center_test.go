package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
)

type centerAnnouncementRepo struct {
	AnnouncementRepository
	items   []Announcement
	bodyIDs []int64
}

func (r *centerAnnouncementRepo) ListCenterCandidates(context.Context, time.Time) ([]Announcement, error) {
	items := append([]Announcement(nil), r.items...)
	for i := range items {
		items[i].Content = ""
	}
	return items, nil
}
func (r *centerAnnouncementRepo) GetCenterItems(_ context.Context, ids []int64) ([]Announcement, error) {
	r.bodyIDs = append([]int64(nil), ids...)
	wanted := map[int64]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	out := []Announcement{}
	for _, a := range r.items {
		if wanted[a.ID] {
			out = append(out, a)
		}
	}
	return out, nil
}
func (r *centerAnnouncementRepo) GetByID(_ context.Context, id int64) (*Announcement, error) {
	for _, a := range r.items {
		if a.ID == id {
			b := a
			return &b, nil
		}
	}
	return nil, ErrAnnouncementNotFound
}

type centerUserRepo struct {
	UserRepository
	balance float64
}

func (r *centerUserRepo) GetByID(context.Context, int64) (*User, error) {
	return &User{ID: 7, Balance: r.balance}, nil
}

type centerSubRepo struct{ UserSubscriptionRepository }

func (*centerSubRepo) ListActiveByUserID(context.Context, int64) ([]UserSubscription, error) {
	return nil, nil
}

type centerReadRepo struct {
	AnnouncementReadRepository
	reads map[int64]time.Time
}

func (r *centerReadRepo) GetReadMapByUser(_ context.Context, _ int64, ids []int64) (map[int64]time.Time, error) {
	out := map[int64]time.Time{}
	for _, id := range ids {
		if t, ok := r.reads[id]; ok {
			out[id] = t
		}
	}
	return out, nil
}
func (r *centerReadRepo) MarkRead(_ context.Context, id, _ int64, at time.Time) error {
	if _, ok := r.reads[id]; !ok {
		r.reads[id] = at
	}
	return nil
}
func (r *centerReadRepo) MarkReadRevisions(ctx context.Context, userID int64, revisions []AnnouncementReadRevision, at time.Time) ([]int64, error) {
	ids := []int64{}
	for _, a := range revisions {
		if _, ok := r.reads[a.ID]; !ok {
			_ = r.MarkRead(ctx, a.ID, userID, at)
			ids = append(ids, a.ID)
		}
	}
	return ids, nil
}
func centerFixture() (*AnnouncementService, *centerAnnouncementRepo, *centerUserRepo, *centerReadRepo) {
	r := &centerAnnouncementRepo{}
	u := &centerUserRepo{}
	reads := &centerReadRepo{reads: map[int64]time.Time{}}
	s := NewAnnouncementService(r, reads, u, &centerSubRepo{})
	s.SetSnapshotKey("unit-test-shared-secret")
	return s, r, u, reads
}
func centerItem(id int64, mode string, created time.Time) Announcement {
	return Announcement{ID: id, Title: fmt.Sprint(id), Content: "markdown body", DisplayMode: mode, Status: AnnouncementStatusActive, CreatedAt: created, UpdatedAt: created}
}

func TestAnnouncementCenterPaginatesAfterTargetingAndCountsBeyondLegacyCaps(t *testing.T) {
	s, r, _, reads := centerFixture()
	now := time.Now().Add(-time.Hour)
	for i := int64(1); i <= 275; i++ {
		r.items = append(r.items, centerItem(i, AnnouncementDisplayTimeline, now.Add(time.Duration(i)*time.Second)))
	}
	r.items = append(r.items, centerItem(500, AnnouncementDisplayNotice, now))
	hidden := centerItem(600, AnnouncementDisplayTimeline, now.Add(1000*time.Second))
	hidden.Targeting = AnnouncementTargeting{AnyOf: []AnnouncementConditionGroup{{AllOf: []AnnouncementCondition{{Type: AnnouncementConditionTypeBalance, Operator: AnnouncementOperatorGT, Value: 100}}}}}
	r.items = append(r.items, hidden)
	reads.reads[275] = now
	summary, err := s.CenterSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 1, summary.NoticeCount)
	require.Equal(t, 275, summary.TimelineCount)
	require.Equal(t, 274, summary.TimelineUnreadCount)
	require.Empty(t, r.bodyIDs, "summary without popups must not read every Markdown body")
	items, page, err := s.ListCenterForUser(context.Background(), 7, AnnouncementDisplayTimeline, pagination.PaginationParams{Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(275), page.Total)
	require.Len(t, items, 20)
	require.Equal(t, int64(275), items[0].Announcement.ID, "read items keep chronological placement")
	require.NotEmpty(t, items[0].Announcement.Content)
	items, _, err = s.ListCenterForUser(context.Background(), 7, AnnouncementDisplayTimeline, pagination.PaginationParams{Page: 14, PageSize: 20})
	require.NoError(t, err)
	require.Len(t, items, 15)
	require.Equal(t, int64(1), items[14].Announcement.ID)
	result, err := s.MarkAllTimelineRead(context.Background(), 7, summary.Snapshot)
	require.NoError(t, err)
	require.Len(t, result.MarkedIDs, 274)
	require.Zero(t, result.Summary.TimelineUnreadCount)
	require.NotContains(t, reads.reads, int64(500))
	require.NotContains(t, reads.reads, int64(600))
	require.Equal(t, now, reads.reads[275], "existing first read stays unchanged")
}
func TestAnnouncementCenterReadSnapshotDoesNotConsumeLaterAnnouncementsOrNewAudience(t *testing.T) {
	s, r, u, reads := centerFixture()
	now := time.Now().Add(-time.Hour)
	for i := int64(1); i <= 4; i++ {
		r.items = append(r.items, centerItem(i, AnnouncementDisplayTimeline, now))
	}
	r.items[2].Status = AnnouncementStatusDraft
	r.items[3].Targeting = AnnouncementTargeting{AnyOf: []AnnouncementConditionGroup{{AllOf: []AnnouncementCondition{{Type: AnnouncementConditionTypeBalance, Operator: AnnouncementOperatorGT, Value: 100}}}}}
	summary, err := s.CenterSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, 2, summary.TimelineUnreadCount)
	r.items[1].Content = "edited later"
	r.items[1].UpdatedAt = time.Now()
	r.items[2].Status = AnnouncementStatusActive
	r.items[2].UpdatedAt = time.Now()
	u.balance = 200
	r.items = append(r.items, centerItem(5, AnnouncementDisplayTimeline, time.Now()))
	result, err := s.MarkAllTimelineRead(context.Background(), 7, summary.Snapshot)
	require.NoError(t, err)
	require.Equal(t, []int64{1}, result.MarkedIDs)
	require.Len(t, reads.reads, 1)
	require.Equal(t, 4, result.Summary.TimelineUnreadCount)
}
func TestAnnouncementCenterSnapshotRejectsTamperingUserSwitchExpiryAndFuture(t *testing.T) {
	s, r, _, _ := centerFixture()
	r.items = []Announcement{centerItem(1, AnnouncementDisplayTimeline, time.Now().Add(-time.Hour))}
	summary, err := s.CenterSummary(context.Background(), 7)
	require.NoError(t, err)
	_, err = s.MarkAllTimelineRead(context.Background(), 8, summary.Snapshot)
	require.ErrorIs(t, err, ErrAnnouncementInvalidSnapshot)
	_, err = s.MarkAllTimelineRead(context.Background(), 7, summary.Snapshot+"x")
	require.ErrorIs(t, err, ErrAnnouncementInvalidSnapshot)
	for _, at := range []time.Time{time.Now().Add(-3 * time.Hour), time.Now().Add(time.Minute)} {
		token, e := s.signCenterSnapshot(7, at, nil)
		require.NoError(t, e)
		_, err = s.MarkAllTimelineRead(context.Background(), 7, token)
		require.ErrorIs(t, err, ErrAnnouncementInvalidSnapshot)
	}
	other, _, _, _ := centerFixture()
	_, err = other.parseCenterSnapshot(7, summary.Snapshot, time.Now())
	require.NoError(t, err, "same managed secret works across instances")
}
func TestAnnouncementCenterReadChecksCurrentVisibilityAndDoesNotAutoReadGet(t *testing.T) {
	s, r, _, reads := centerFixture()
	now := time.Now().Add(-time.Hour)
	r.items = []Announcement{centerItem(1, AnnouncementDisplayNotice, now), centerItem(2, AnnouncementDisplayTimeline, now)}
	r.items[0].NotifyMode = AnnouncementNotifyModePopup
	item, err := s.GetForUser(context.Background(), 7, 1)
	require.NoError(t, err)
	require.NotEmpty(t, item.Announcement.Content)
	require.Empty(t, reads.reads)
	summary, err := s.CenterSummary(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, summary.PopupCandidates, 1)
	require.Equal(t, 1, summary.TimelineUnreadCount)
	r.items[1].Status = AnnouncementStatusArchived
	result, err := s.MarkAllTimelineRead(context.Background(), 7, summary.Snapshot)
	require.NoError(t, err)
	require.Empty(t, result.MarkedIDs)
	require.Empty(t, reads.reads)
	r.items[0].EndsAt = &now
	_, err = s.GetForUser(context.Background(), 7, 1)
	require.ErrorIs(t, err, ErrAnnouncementNotFound)
}
func TestAnnouncementCreateDisplayDefaultsAndValidation(t *testing.T) {
	repo := &announcementRepoStub{}
	s := NewAnnouncementService(repo, nil, nil, nil)
	a, err := s.Create(context.Background(), &CreateAnnouncementInput{Title: "notice", Content: "body"})
	require.NoError(t, err)
	require.Equal(t, AnnouncementDisplayTimeline, a.DisplayMode)
	_, err = s.Create(context.Background(), &CreateAnnouncementInput{Title: "notice", Content: "body", DisplayMode: "both"})
	require.ErrorIs(t, err, ErrAnnouncementInvalidDisplayMode)
	mode := AnnouncementDisplayNotice
	a, err = s.Update(context.Background(), 1, &UpdateAnnouncementInput{DisplayMode: &mode})
	require.NoError(t, err)
	require.Equal(t, AnnouncementDisplayNotice, a.DisplayMode)
}

func TestAnnouncementCenterBulkReadConfirmsCrossDeviceReadsWithoutChangingFirstTimestamp(t *testing.T) {
	s, r, _, reads := centerFixture()
	now := time.Now().Add(-time.Hour)
	r.items = []Announcement{centerItem(1, AnnouncementDisplayTimeline, now), centerItem(2, AnnouncementDisplayTimeline, now)}
	summary, err := s.CenterSummary(context.Background(), 7)
	require.NoError(t, err)
	otherDeviceRead := time.Now().Add(-time.Minute)
	reads.reads[1] = otherDeviceRead
	result, err := s.MarkAllTimelineRead(context.Background(), 7, summary.Snapshot)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{1, 2}, result.MarkedIDs)
	require.Zero(t, result.Summary.TimelineUnreadCount)
	require.Equal(t, otherDeviceRead, reads.reads[1])
	result, err = s.MarkAllTimelineRead(context.Background(), 7, summary.Snapshot)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{1, 2}, result.MarkedIDs, "repeating an old snapshot still confirms both read items to the current device")
	require.Equal(t, otherDeviceRead, reads.reads[1])
}
