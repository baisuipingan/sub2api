package service

import (
	"context"
	"errors"
	"time"
)

const (
	OfficeResourceMaxBytes  int64 = 24 << 20
	OfficeWorkspaceMaxFiles       = 64
	OfficeResourceUserBytes int64 = 1 << 30
	OfficeResourceUserFiles       = 1000
)

var (
	ErrOfficeResourceQuota      = errors.New("office resource quota exceeded")
	ErrOfficeResourceType       = errors.New("unsupported or invalid office file; use PDF, DOCX, XLSX, PPTX, PNG, JPEG, WebP, UTF-8 text, Markdown, CSV or JSON")
	ErrOfficeResourceTooLarge   = errors.New("office files and task workspace must not exceed 24 MiB or 64 files")
	ErrOfficeStorageUnavailable = errors.New("office files require configured private S3/R2 storage")
)

// Object keys and targets are internal metadata. Clients receive only opaque
// resource/version identifiers and short-lived, owner-authorized access URLs.
type OfficeResource struct {
	ID              string    `json:"id"`
	VersionID       string    `json:"version_id"`
	ParentVersionID string    `json:"parent_version_id,omitempty"`
	Name            string    `json:"name"`
	MIME            string    `json:"mime"`
	Bytes           int64     `json:"bytes"`
	SHA256          string    `json:"sha256"`
	PreviewKind     string    `json:"preview_kind"`
	Source          string    `json:"source"`
	Role            string    `json:"role"`
	CreatedAt       time.Time `json:"created_at"`
	UserID          int64     `json:"-"`
	State           string    `json:"-"`
	PublicationID   string    `json:"-"`
	RunID           string    `json:"-"`
	GalleryImageID  string    `json:"-"`
	Endpoint        string    `json:"-"`
	Region          string    `json:"-"`
	Bucket          string    `json:"-"`
	ObjectKey       string    `json:"-"`
	PreviewKey      string    `json:"-"`
	PreviewMIME     string    `json:"-"`
	PreviewBytes    int64     `json:"-"`
	PreviewSHA256   string    `json:"-"`
}

type OfficeWorkspaceFile struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	SHA256        string `json:"sha256"`
	MIME          string `json:"mime"`
	ContentBase64 string `json:"content_base64"`
}

type OfficeWorkspace struct {
	Version     int64                 `json:"version"`
	HeadVersion int64                 `json:"head_version"`
	SnapshotID  string                `json:"snapshot_id"`
	Files       []OfficeWorkspaceFile `json:"files"`
}

type OfficeWorkspaceSave struct {
	OperationID     string                `json:"operation_id"`
	ExpectedVersion int64                 `json:"expected_version"`
	Files           []OfficeWorkspaceFile `json:"files"`
}

type OfficeSnapshot struct {
	ID          string
	UserID      int64
	RunID       string
	OperationID string
	Version     int64
	Hash        string
	Bytes       int64
	State       string
	Endpoint    string
	Region      string
	Bucket      string
	ObjectKey   string
}

type OfficeArtifactPublish struct {
	PublicationID    string `json:"publication_id"`
	Path             string `json:"path"`
	SHA256           string `json:"sha256"`
	MIME             string `json:"mime"`
	Size             int64  `json:"size"`
	WorkspaceVersion int64  `json:"workspace_version"`
	ResourceID       string `json:"resource_id,omitempty"`
	ParentVersionID  string `json:"parent_version_id,omitempty"`
	PreviewPath      string `json:"preview_path,omitempty"`
}

type OfficeResourceAccess struct {
	URL                   string    `json:"url"`
	PreviewURL            string    `json:"preview_url,omitempty"`
	ExpiresAt             time.Time `json:"expires_at"`
	PreviewKind           string    `json:"preview_kind"`
	CanRenderPrintPreview bool      `json:"can_render_print_preview,omitempty"`
}

type OfficeResourceCleanup struct {
	Kind     string
	ID       string
	UserID   int64
	Endpoint string
	Region   string
	Bucket   string
	Keys     []string
}

type OfficeResourcesRepository interface {
	ListGalleryImages(context.Context, int64, string, int) ([]OfficeResource, string, error)
	AttachGalleryImage(context.Context, int64, string) (*OfficeResource, error)
	ListResources(context.Context, int64, string, int) ([]OfficeResource, string, error)
	GetResourceVersion(context.Context, int64, string, bool) (*OfficeResource, error)
	ResolveVersions(context.Context, int64, []string) ([]OfficeResourceVersion, error)
	ReserveVersion(context.Context, OfficeResource, *OfficeRunLease) (*OfficeResource, error)
	FinishVersion(context.Context, int64, string, *OfficeRunLease) (*OfficeResource, error)
	StorePrintPreview(context.Context, int64, string, string, int64, string, *OfficeDispatch, func() error) error
	DeleteResource(context.Context, int64, string) error
	PinRunInputs(context.Context, OfficeRun) error
	LatestSnapshot(context.Context, OfficeRunLease, int64) (*OfficeSnapshot, error)
	ReserveSnapshot(context.Context, OfficeRunLease, OfficeSnapshot, int64) (*OfficeSnapshot, error)
	FinishSnapshot(context.Context, OfficeRunLease, string) (*OfficeSnapshot, error)
	HasStorage(context.Context) (bool, error)
	ListCleanup(context.Context, int) ([]OfficeResourceCleanup, error)
	FinalizeCleanup(context.Context, OfficeResourceCleanup) error
}
