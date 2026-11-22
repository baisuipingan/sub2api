package service

import (
	"context"
	"time"
)

type WorkbenchGalleryFilter struct {
	Page, PageSize int
	UserID         int64
	TaskID         string
}

// These projections deliberately omit task JSON, prompts and storage keys.
type WorkbenchGalleryTask struct {
	UserID      int64     `json:"user_id"`
	UserEmail   string    `json:"user_email"`
	TaskID      string    `json:"task_id"`
	Model       string    `json:"model"`
	CreatedAt   time.Time `json:"created_at"`
	OutputCount int64     `json:"output_count"`
	ImageIDs    []string  `json:"image_ids"`
}

type WorkbenchGalleryPage struct {
	Items    []WorkbenchGalleryTask `json:"items"`
	Total    int64                  `json:"total"`
	Page     int                    `json:"page"`
	PageSize int                    `json:"page_size"`
	Pages    int64                  `json:"pages"`
}

type WorkbenchGalleryRepository interface {
	ListGalleryTasks(context.Context, WorkbenchGalleryFilter) (*WorkbenchGalleryPage, error)
	GalleryOutputExists(context.Context, int64, string, string) (bool, error)
}

func ProvideWorkbenchAdminService(repo WorkbenchAdminRepository, settings *SettingService, groups GroupRepository, gallery *ImageStudioGalleryService) *WorkbenchAdminService {
	svc := NewWorkbenchAdminService(repo, settings, groups)
	svc.gallery = gallery
	return svc
}

func (s *WorkbenchAdminService) ListGallery(ctx context.Context, f WorkbenchGalleryFilter) (*WorkbenchGalleryPage, error) {
	if f.Page < 1 || f.Page > 10000 || f.PageSize < 1 || f.PageSize > 50 || f.UserID < 0 || f.TaskID != "" && !galleryIDValid(f.TaskID) {
		return nil, ErrWorkbenchInvalid
	}
	repo, ok := s.repo.(WorkbenchGalleryRepository)
	if !ok {
		return nil, ErrGalleryUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return repo.ListGalleryTasks(ctx, f)
}

func (s *WorkbenchAdminService) GalleryImageURL(ctx context.Context, userID int64, taskID, imageID, variant string) (GalleryAssetURL, error) {
	if userID <= 0 || !galleryIDValid(taskID) || !galleryIDValid(imageID) || variant != "thumbnail" && variant != "preview" {
		return GalleryAssetURL{}, ErrWorkbenchInvalid
	}
	repo, ok := s.repo.(WorkbenchGalleryRepository)
	if !ok || s.gallery == nil {
		return GalleryAssetURL{}, ErrGalleryUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	exists, err := repo.GalleryOutputExists(ctx, userID, taskID, imageID)
	if err != nil {
		return GalleryAssetURL{}, err
	}
	if !exists {
		return GalleryAssetURL{}, ErrGalleryNotFound
	}
	// AssetURL rechecks owner, ready state, private target and variant metadata.
	return s.gallery.AssetURL(ctx, userID, imageID, variant)
}
