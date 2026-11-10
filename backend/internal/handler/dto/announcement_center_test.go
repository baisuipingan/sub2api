package dto

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAnnouncementDisplayModeDefaultsAndRetainsReadState(t *testing.T) {
	readAt := time.Date(2026, time.November, 10, 0, 0, 0, 0, time.UTC)
	for _, mode := range []string{"", "timeline", "notice"} {
		announcement := service.Announcement{ID: 7, Title: "Title", Content: "Body", NotifyMode: "popup", DisplayMode: mode}
		admin := AnnouncementFromService(&announcement)
		user := UserAnnouncementFromService(&service.UserAnnouncement{Announcement: announcement, ReadAt: &readAt})
		want := "timeline"
		if mode == "notice" {
			want = "notice"
		}
		require.Equal(t, want, admin.DisplayMode)
		require.Equal(t, want, user.DisplayMode)
		require.Equal(t, "popup", user.NotifyMode, "display location must not overwrite reminder mode")
		require.Equal(t, &readAt, user.ReadAt)
	}
	require.Nil(t, AnnouncementFromService(nil))
	require.Nil(t, UserAnnouncementFromService(nil))
}

func TestAnnouncementCenterSummaryJSONContract(t *testing.T) {
	summary := AnnouncementCenterSummaryFromService(&service.AnnouncementCenterSummary{
		NoticeCount: 1, TimelineCount: 27, TimelineUnreadCount: 23, Snapshot: "signed-snapshot",
		PopupCandidates: []service.UserAnnouncement{{Announcement: service.Announcement{ID: 7, DisplayMode: "notice", NotifyMode: "popup"}}},
	})
	require.Equal(t, 23, summary.TimelineUnreadCount, "the count is complete, not capped by a 20-item page")
	raw, err := json.Marshal(summary)
	require.NoError(t, err)
	var actual map[string]any
	require.NoError(t, json.Unmarshal(raw, &actual))
	require.Equal(t, float64(1), actual["notice_count"])
	require.Equal(t, float64(27), actual["timeline_count"])
	require.Equal(t, float64(23), actual["timeline_unread_count"])
	require.Equal(t, "signed-snapshot", actual["snapshot"])
	popups, ok := actual["popup_candidates"].([]any)
	require.True(t, ok)
	require.Len(t, popups, 1)
	popup, ok := popups[0].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "notice", popup["display_mode"])
	empty := AnnouncementCenterSummaryFromService(&service.AnnouncementCenterSummary{})
	raw, err = json.Marshal(empty)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"popup_candidates":[]`)
	require.Nil(t, AnnouncementCenterSummaryFromService(nil))
}
