package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type workbenchGalleryStub struct {
	WorkbenchAdminRepository
	exists          bool
	calls           int
	userID          int64
	taskID, imageID string
}

func (r *workbenchGalleryStub) ListGalleryTasks(context.Context, WorkbenchGalleryFilter) (*WorkbenchGalleryPage, error) {
	r.calls++
	return &WorkbenchGalleryPage{Items: []WorkbenchGalleryTask{}}, nil
}
func (r *workbenchGalleryStub) GalleryOutputExists(_ context.Context, userID int64, taskID, imageID string) (bool, error) {
	r.calls++
	r.userID = userID
	r.taskID = taskID
	r.imageID = imageID
	return r.exists, nil
}

func TestWorkbenchGalleryValidatesBeforeReadingPrivateAssets(t *testing.T) {
	repo := &workbenchGalleryStub{}
	svc := NewWorkbenchAdminService(repo, nil, nil)
	for _, f := range []WorkbenchGalleryFilter{{Page: 0, PageSize: 20}, {Page: 1, PageSize: 51}, {Page: 1, PageSize: 20, UserID: -1}, {Page: 1, PageSize: 20, TaskID: "bad/id"}} {
		_, err := svc.ListGallery(context.Background(), f)
		require.ErrorIs(t, err, ErrWorkbenchInvalid)
	}
	for _, variant := range []string{"original", "", "Original", "thumbnail/../original"} {
		_, err := svc.GalleryImageURL(context.Background(), 7, "task-one", "image-one", variant)
		require.ErrorIs(t, err, ErrWorkbenchInvalid)
	}
	require.Zero(t, repo.calls)
}

func TestWorkbenchGalleryRequiresOutputAssociationBeforeSigning(t *testing.T) {
	repo := &workbenchGalleryStub{}
	svc := NewWorkbenchAdminService(repo, nil, nil)
	// Non-nil service is enough: a missing association must return before any
	// asset lookup or storage access, including references owned by this user.
	svc.gallery = &ImageStudioGalleryService{}
	_, err := svc.GalleryImageURL(context.Background(), 7, "task-one", "image-reference", "thumbnail")
	require.ErrorIs(t, err, ErrGalleryNotFound)
	require.Equal(t, int64(7), repo.userID)
	require.Equal(t, "task-one", repo.taskID)
	require.Equal(t, "image-reference", repo.imageID)
}
