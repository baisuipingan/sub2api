package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
)

const announcementSnapshotMaxBytes = 200 << 10
const announcementSnapshotLifetime = 2 * time.Hour

type AnnouncementCenterSummary struct {
	NoticeCount         int                `json:"notice_count"`
	TimelineCount       int                `json:"timeline_count"`
	TimelineUnreadCount int                `json:"timeline_unread_count"`
	Snapshot            string             `json:"snapshot"`
	PopupCandidates     []UserAnnouncement `json:"popup_candidates"`
}
type AnnouncementCenterReadResult struct {
	MarkedIDs []int64                   `json:"marked_ids"`
	Summary   AnnouncementCenterSummary `json:"summary"`
}
type announcementSnapshot struct {
	UserID   int64 `json:"u"`
	IssuedAt int64 `json:"t"`
	// Pairs are [announcement ID, revision microseconds]. IDs are captured from
	// this user's visible timeline, not reconstructed from mutable future rules.
	Revisions [][2]int64 `json:"r"`
}
type announcementAudience struct {
	balance       float64
	subscriptions map[int64]struct{}
}

// SetSnapshotKey shares the existing managed JWT secret across application
// instances; no new operator credential or snapshot database is introduced.
func (s *AnnouncementService) SetSnapshotKey(secret string) {
	sum := sha256.Sum256([]byte("announcement-center-snapshot-v1\n" + secret))
	s.snapshotKey = append([]byte(nil), sum[:]...)
}
func announcementDisplayMode(a Announcement) string {
	if a.DisplayMode == AnnouncementDisplayNotice {
		return AnnouncementDisplayNotice
	}
	return AnnouncementDisplayTimeline
}
func (s *AnnouncementService) centerAudience(ctx context.Context, userID int64) (announcementAudience, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return announcementAudience{}, fmt.Errorf("get announcement viewer: %w", err)
	}
	subs, err := s.userSubRepo.ListActiveByUserID(ctx, userID)
	if err != nil {
		return announcementAudience{}, fmt.Errorf("get announcement audience: %w", err)
	}
	groups := make(map[int64]struct{}, len(subs))
	for _, sub := range subs {
		groups[sub.GroupID] = struct{}{}
	}
	return announcementAudience{user.Balance, groups}, nil
}
func (a announcementAudience) visible(item Announcement, now time.Time) bool {
	return item.IsActiveAt(now) && item.Targeting.Matches(a.balance, a.subscriptions)
}
func (s *AnnouncementService) centerCandidates(ctx context.Context, userID int64, now time.Time) ([]UserAnnouncement, announcementAudience, error) {
	audience, err := s.centerAudience(ctx, userID)
	if err != nil {
		return nil, audience, err
	}
	var items []Announcement
	if repo, ok := s.announcementRepo.(AnnouncementCenterRepository); ok {
		items, err = repo.ListCenterCandidates(ctx, now)
	} else {
		items, err = s.announcementRepo.ListActive(ctx, now)
	}
	if err != nil {
		return nil, audience, err
	}
	visible := make([]Announcement, 0, len(items))
	ids := make([]int64, 0, len(items))
	for _, item := range items {
		if audience.visible(item, now) {
			visible = append(visible, item)
			ids = append(ids, item.ID)
		}
	}
	reads, err := s.readRepo.GetReadMapByUser(ctx, userID, ids)
	if err != nil {
		return nil, audience, err
	}
	out := make([]UserAnnouncement, 0, len(visible))
	for _, item := range visible {
		var ptr *time.Time
		if t, ok := reads[item.ID]; ok {
			v := t
			ptr = &v
		}
		out = append(out, UserAnnouncement{item, ptr})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Announcement, out[j].Announcement
		if a.CreatedAt.Equal(b.CreatedAt) {
			return a.ID > b.ID
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
	return out, audience, nil
}
func (s *AnnouncementService) centerBodies(ctx context.Context, selected []UserAnnouncement, audience announcementAudience, now time.Time) ([]UserAnnouncement, error) {
	if len(selected) == 0 {
		return []UserAnnouncement{}, nil
	}
	repo, ok := s.announcementRepo.(AnnouncementCenterRepository)
	if !ok {
		return selected, nil
	}
	ids := make([]int64, 0, len(selected))
	for _, item := range selected {
		ids = append(ids, item.Announcement.ID)
	}
	bodies, err := repo.GetCenterItems(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Announcement, len(bodies))
	for _, a := range bodies {
		byID[a.ID] = a
	}
	result := make([]UserAnnouncement, 0, len(selected))
	for _, a := range selected {
		body, ok := byID[a.Announcement.ID]
		if ok && audience.visible(body, now) && announcementDisplayMode(body) == announcementDisplayMode(a.Announcement) {
			a.Announcement = body
			result = append(result, a)
		}
	}
	return result, nil
}
func (s *AnnouncementService) signCenterSnapshot(userID int64, now time.Time, items []UserAnnouncement) (string, error) {
	if len(s.snapshotKey) == 0 {
		return "", fmt.Errorf("announcement snapshot signing key is unavailable")
	}
	snapshot := announcementSnapshot{UserID: userID, IssuedAt: now.UnixMicro(), Revisions: make([][2]int64, 0)}
	for _, item := range items {
		if item.ReadAt == nil && announcementDisplayMode(item.Announcement) == AnnouncementDisplayTimeline {
			snapshot.Revisions = append(snapshot.Revisions, [2]int64{item.Announcement.ID, item.Announcement.UpdatedAt.UnixMicro()})
		}
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.snapshotKey)
	_, _ = mac.Write([]byte(encoded))
	token := encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if len(token) > announcementSnapshotMaxBytes {
		return "", fmt.Errorf("announcement snapshot exceeds supported size")
	}
	return token, nil
}
func (s *AnnouncementService) parseCenterSnapshot(userID int64, token string, now time.Time) (announcementSnapshot, error) {
	var snapshot announcementSnapshot
	if token == "" || len(token) > announcementSnapshotMaxBytes || len(s.snapshotKey) == 0 {
		return snapshot, ErrAnnouncementInvalidSnapshot
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return snapshot, ErrAnnouncementInvalidSnapshot
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return snapshot, ErrAnnouncementInvalidSnapshot
	}
	mac := hmac.New(sha256.New, s.snapshotKey)
	_, _ = mac.Write([]byte(parts[0]))
	if !hmac.Equal(sig, mac.Sum(nil)) {
		return snapshot, ErrAnnouncementInvalidSnapshot
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &snapshot) != nil {
		return snapshot, ErrAnnouncementInvalidSnapshot
	}
	issued := time.UnixMicro(snapshot.IssuedAt)
	if snapshot.UserID != userID || issued.After(now) || now.Sub(issued) > announcementSnapshotLifetime {
		return snapshot, ErrAnnouncementInvalidSnapshot
	}
	return snapshot, nil
}
func (s *AnnouncementService) CenterSummary(ctx context.Context, userID int64) (*AnnouncementCenterSummary, error) {
	now := time.Now()
	items, audience, err := s.centerCandidates(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	summary := &AnnouncementCenterSummary{PopupCandidates: []UserAnnouncement{}}
	popups := make([]UserAnnouncement, 0)
	for _, item := range items {
		if announcementDisplayMode(item.Announcement) == AnnouncementDisplayNotice {
			summary.NoticeCount++
		} else {
			summary.TimelineCount++
			if item.ReadAt == nil {
				summary.TimelineUnreadCount++
			}
		}
		// Batch popups retain the previous small queue size. Remaining unread popup
		// items become candidates after earlier ones are acknowledged and refreshed.
		if item.ReadAt == nil && item.Announcement.NotifyMode == AnnouncementNotifyModePopup && len(popups) < 20 {
			popups = append(popups, item)
		}
	}
	summary.Snapshot, err = s.signCenterSnapshot(userID, now, items)
	if err != nil {
		return nil, err
	}
	summary.PopupCandidates, err = s.centerBodies(ctx, popups, audience, now)
	if err != nil {
		return nil, err
	}
	return summary, nil
}
func (s *AnnouncementService) ListCenterForUser(ctx context.Context, userID int64, displayMode string, params pagination.PaginationParams) ([]UserAnnouncement, *pagination.PaginationResult, error) {
	if !isValidAnnouncementDisplayMode(displayMode) {
		return nil, nil, ErrAnnouncementInvalidDisplayMode
	}
	if params.Page < 1 {
		params.Page = 1
	}
	if params.PageSize < 1 {
		params.PageSize = 20
	}
	if params.PageSize > 100 {
		params.PageSize = 100
	}
	now := time.Now()
	items, audience, err := s.centerCandidates(ctx, userID, now)
	if err != nil {
		return nil, nil, err
	}
	filtered := make([]UserAnnouncement, 0)
	for _, item := range items {
		if announcementDisplayMode(item.Announcement) == displayMode {
			filtered = append(filtered, item)
		}
	}
	total := len(filtered)
	page := &pagination.PaginationResult{Total: int64(total), Page: params.Page, PageSize: params.PageSize, Pages: (total + params.PageSize - 1) / params.PageSize}
	offset := params.Offset()
	if offset >= total {
		return []UserAnnouncement{}, page, nil
	}
	end := min(offset+params.PageSize, total)
	bodies, err := s.centerBodies(ctx, filtered[offset:end], audience, now)
	return bodies, page, err
}
func (s *AnnouncementService) GetForUser(ctx context.Context, userID, id int64) (*UserAnnouncement, error) {
	audience, err := s.centerAudience(ctx, userID)
	if err != nil {
		return nil, err
	}
	item, err := s.announcementRepo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if !audience.visible(*item, time.Now()) {
		return nil, ErrAnnouncementNotFound
	}
	reads, err := s.readRepo.GetReadMapByUser(ctx, userID, []int64{id})
	if err != nil {
		return nil, err
	}
	var ptr *time.Time
	if t, ok := reads[id]; ok {
		ptr = &t
	}
	return &UserAnnouncement{*item, ptr}, nil
}
func (s *AnnouncementService) MarkAllTimelineRead(ctx context.Context, userID int64, token string) (*AnnouncementCenterReadResult, error) {
	now := time.Now()
	snapshot, err := s.parseCenterSnapshot(userID, token, now)
	if err != nil {
		return nil, err
	}
	items, _, err := s.centerCandidates(ctx, userID, now)
	if err != nil {
		return nil, err
	}
	revisions := make(map[int64]int64, len(snapshot.Revisions))
	for _, r := range snapshot.Revisions {
		revisions[r[0]] = r[1]
	}
	eligible := make([]AnnouncementReadRevision, 0)
	matchedIDs := make([]int64, 0)
	for _, item := range items {
		a := item.Announcement
		revision, existed := revisions[a.ID]
		if !existed || announcementDisplayMode(a) != AnnouncementDisplayTimeline || revision != a.UpdatedAt.UnixMicro() {
			continue
		}
		matchedIDs = append(matchedIDs, a.ID)
		if item.ReadAt == nil {
			eligible = append(eligible, AnnouncementReadRevision{a.ID, a.UpdatedAt})
		}
	}
	if repo, ok := s.readRepo.(AnnouncementCenterReadRepository); ok {
		_, err = repo.MarkReadRevisions(ctx, userID, eligible, now)
	} else {
		for _, revision := range eligible {
			if err = s.readRepo.MarkRead(ctx, revision.ID, userID, now); err != nil {
				break
			}
		}
	}
	if err != nil {
		return nil, err
	}
	// A second device may have read an item before or during this operation.
	// Return confirmed read IDs, including existing rows, without rewriting the
	// database's first-read timestamp or treating stale revisions as read.
	confirmed, err := s.readRepo.GetReadMapByUser(ctx, userID, matchedIDs)
	if err != nil {
		return nil, err
	}
	marked := make([]int64, 0, len(confirmed))
	for _, id := range matchedIDs {
		if _, ok := confirmed[id]; ok {
			marked = append(marked, id)
		}
	}
	summary, err := s.CenterSummary(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &AnnouncementCenterReadResult{marked, *summary}, nil
}
