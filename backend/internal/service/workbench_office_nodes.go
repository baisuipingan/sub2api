package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
)

const (
	OfficeNodeActive      = "active"
	OfficeNodeDraining    = "draining"
	OfficeNodeDisabled    = "disabled"
	OfficeNodeUnchecked   = "unchecked"
	OfficeNodeOnline      = "online"
	OfficeNodeUnavailable = "unavailable"
	officeNodeFreshness   = time.Minute
)

var (
	ErrOfficeNodeInvalid     = errors.New("invalid office node configuration")
	ErrOfficeNodeBusy        = errors.New("office node still has assigned jobs")
	ErrOfficeNodeDuplicate   = errors.New("office node address or identity already registered")
	ErrOfficeNodeUnavailable = errors.New("office node could not be authenticated")
	ErrOfficeNodeIdentity    = errors.New("office node identity changed")
	ErrOfficeNodeSecret      = errors.New("office node credential storage is unavailable")
)

type OfficeNodeStatus struct {
	NodeID              string   `json:"node_id"`
	Version             string   `json:"version"`
	Actions             []string `json:"actions"`
	Concurrency         int      `json:"concurrency"`
	ConcurrencyRevision *int64   `json:"concurrency_revision,omitempty"`
	Running             int      `json:"running"`
	Queued              int      `json:"queued"`
	AwaitingUpload      int      `json:"awaiting_upload"`
	StorageOrigins      []string `json:"storage_origins"`
}

type OfficeNode struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	URL             string            `json:"url"`
	State           string            `json:"state"`
	MaxConcurrent   int               `json:"max_concurrent"`
	TokenConfigured bool              `json:"token_configured"`
	Health          string            `json:"health"`
	LastCheckedAt   *time.Time        `json:"last_checked_at,omitempty"`
	Status          *OfficeNodeStatus `json:"status,omitempty"`
	Assigned        int               `json:"assigned"`
	TokenEncrypted  string            `json:"-"`
	WorkerIdentity  string            `json:"-"`
	Revision        int64             `json:"-"`
}

type OfficeNodeUpdate struct {
	Name          string `json:"name"`
	URL           string `json:"url"`
	Token         string `json:"token"`
	State         string `json:"state"`
	MaxConcurrent int    `json:"max_concurrent"`
}

type OfficeNodeOverview struct {
	Total          int `json:"total"`
	Online         int `json:"online"`
	Draining       int `json:"draining"`
	Running        int `json:"running"`
	Queued         int `json:"queued"`
	Waiting        int `json:"waiting"`
	Assigned       int `json:"assigned"`
	AwaitingUpload int `json:"awaiting_upload"`
	Slots          int `json:"slots"`
}

func summarizeOfficeNodes(nodes []OfficeNode) *OfficeNodeOverview {
	result := &OfficeNodeOverview{Total: len(nodes)}
	for _, node := range nodes {
		result.Assigned += node.Assigned
		if node.State == OfficeNodeDraining {
			result.Draining++
		}
		if !node.officeNodeOnline() || node.State == OfficeNodeDisabled {
			continue
		}
		result.Online++
		result.Running += node.Status.Running
		result.Queued += node.Status.Queued
		result.AwaitingUpload += node.Status.AwaitingUpload
		if node.State == OfficeNodeActive {
			result.Slots += max(0, node.officeNodeCapacity()-max(node.Assigned, node.Status.Running+node.Status.Queued))
		}
	}
	return result
}

type workbenchOfficeNodeRepository interface {
	ListOfficeNodes(context.Context) ([]OfficeNode, error)
	CountWaitingOfficeNodes(context.Context) (int, error)
	GetOfficeNode(context.Context, string) (OfficeNode, error)
	CreateOfficeNode(context.Context, OfficeNode) (OfficeNode, error)
	UpdateOfficeNode(context.Context, OfficeNode) (OfficeNode, error)
	SaveOfficeNodeStatus(context.Context, OfficeNode) (OfficeNode, error)
	DeleteOfficeNode(context.Context, string) error
}

func (s *OfficeAgentService) officeNodeRepository() (workbenchOfficeNodeRepository, error) {
	repo, ok := s.repo.(workbenchOfficeNodeRepository)
	if !ok {
		return nil, ErrOfficeDisabled
	}
	return repo, nil
}

func (s *OfficeAgentService) officeNodes(ctx context.Context) ([]OfficeNode, error) {
	repo, err := s.officeNodeRepository()
	if err != nil {
		return nil, err
	}
	return repo.ListOfficeNodes(ctx)
}

func (s *OfficeAgentService) officeNode(ctx context.Context, id string) (OfficeNode, error) {
	repo, err := s.officeNodeRepository()
	if err != nil {
		return OfficeNode{}, err
	}
	if uuid.Validate(id) != nil {
		return OfficeNode{}, ErrOfficeNodeInvalid
	}
	return repo.GetOfficeNode(ctx, id)
}

func (s *OfficeAgentService) AdminOfficeNodes(ctx context.Context) ([]OfficeNode, error) {
	nodes, err := s.officeNodes(ctx)
	for i := range nodes {
		if nodes[i].Health == OfficeNodeOnline && !nodes[i].officeNodeOnline() {
			nodes[i].Health = OfficeNodeUnavailable
		}
	}
	return nodes, err
}

func (n OfficeNode) officeNodeOnline() bool {
	return n.Health == OfficeNodeOnline && n.LastCheckedAt != nil && n.Status != nil && n.WorkerIdentity != "" && time.Since(*n.LastCheckedAt) < officeNodeFreshness
}

func (n OfficeNode) officeNodeCapacity() int {
	if n.Status == nil {
		return 0
	}
	if revision := n.Status.ConcurrencyRevision; revision != nil && *revision == 0 {
		return 0
	}
	return min(n.MaxConcurrent, n.Status.Concurrency)
}

func (s *OfficeAgentService) SaveAdminOfficeNode(ctx context.Context, id string, in OfficeNodeUpdate) (OfficeNode, error) {
	repo, err := s.officeNodeRepository()
	if err != nil {
		return OfficeNode{}, err
	}
	in.Name = strings.TrimSpace(in.Name)
	in.URL, err = normalizeOfficeNodeOrigin(in.URL)
	if err != nil || in.Name == "" || !utf8.ValidString(in.Name) || len(in.Name) > 120 || strings.IndexFunc(in.Name, unicode.IsControl) >= 0 || in.MaxConcurrent < 1 || in.MaxConcurrent > 8 {
		return OfficeNode{}, ErrOfficeNodeInvalid
	}
	switch in.State {
	case OfficeNodeActive, OfficeNodeDraining, OfficeNodeDisabled:
	default:
		return OfficeNode{}, ErrOfficeNodeInvalid
	}
	node := OfficeNode{ID: uuid.NewString(), Health: OfficeNodeUnchecked}
	if id != "" {
		node, err = s.officeNode(ctx, id)
		if err != nil {
			return OfficeNode{}, err
		}
		if node.URL != in.URL && in.Token == "" {
			return OfficeNode{}, ErrOfficeNodeInvalid
		}
	}
	connectionChanged := node.URL != in.URL || in.Token != ""
	if node.Assigned > 0 && (connectionChanged || in.State == OfficeNodeDisabled) {
		return OfficeNode{}, ErrOfficeNodeBusy
	}
	needsEnableProbe := in.State == OfficeNodeActive && (node.State != OfficeNodeActive || connectionChanged)
	capacityChanged := node.MaxConcurrent != in.MaxConcurrent
	node.Name, node.URL, node.State, node.MaxConcurrent = in.Name, in.URL, in.State, in.MaxConcurrent
	if in.Token != "" {
		if !validOfficeNodeToken(in.Token) {
			return OfficeNode{}, ErrOfficeNodeInvalid
		}
		if !s.canStoreOfficeNodeToken() {
			return OfficeNode{}, ErrOfficeNodeSecret
		}
		node.TokenEncrypted, err = s.officeNodeEncryptor.Encrypt(node.ID + ":" + in.Token)
		if err != nil {
			return OfficeNode{}, ErrOfficeNodeSecret
		}
	}
	if node.TokenEncrypted == "" {
		return OfficeNode{}, ErrOfficeNodeInvalid
	}
	if connectionChanged {
		node.Health, node.LastCheckedAt, node.Status = OfficeNodeUnchecked, nil, nil
	}
	if needsEnableProbe && !node.officeNodeOnline() {
		status, err := s.probeOfficeNode(ctx, node)
		if err != nil {
			return OfficeNode{}, err
		}
		node = recordOfficeNodeProbe(node, &status, time.Now().UTC())
	}
	if id == "" {
		node, err = repo.CreateOfficeNode(ctx, node)
	} else {
		node, err = repo.UpdateOfficeNode(ctx, node)
	}
	if err == nil && (capacityChanged || needsEnableProbe) && node.Status != nil && node.Status.ConcurrencyRevision != nil {
		// PostgreSQL owns the desired limit. Preserve the save on a transient node
		// failure and retry from the recurring status check or an admin probe.
		if synced, syncErr := s.refreshOfficeNodeStatus(ctx, node); syncErr == nil {
			return synced, nil
		}
	}
	return node, err
}

func (s *OfficeAgentService) DeleteAdminOfficeNode(ctx context.Context, id string) error {
	repo, err := s.officeNodeRepository()
	if err != nil {
		return err
	}
	if uuid.Validate(id) != nil {
		return ErrOfficeNodeInvalid
	}
	return repo.DeleteOfficeNode(ctx, id)
}

func (s *OfficeAgentService) CheckAdminOfficeNode(ctx context.Context, id string) (OfficeNode, error) {
	node, err := s.officeNode(ctx, id)
	if err != nil {
		return OfficeNode{}, err
	}
	return s.refreshOfficeNodeStatus(ctx, node)
}

func (s *OfficeAgentService) refreshOfficeNodeStatus(ctx context.Context, node OfficeNode) (OfficeNode, error) {
	repo, err := s.officeNodeRepository()
	if err != nil {
		return OfficeNode{}, err
	}
	started := time.Now().UTC()
	status, err := s.probeOfficeNode(ctx, node)
	if err != nil {
		return repo.SaveOfficeNodeStatus(ctx, recordOfficeNodeProbe(node, nil, started))
	}
	if revision := status.ConcurrencyRevision; revision != nil && *revision < node.Revision {
		node.WorkerIdentity = status.NodeID
		if applied, err := s.applyOfficeNodeConcurrency(ctx, node); err == nil {
			status = applied
		}
	}
	return repo.SaveOfficeNodeStatus(ctx, recordOfficeNodeProbe(node, &status, started))
}

func recordOfficeNodeProbe(node OfficeNode, status *OfficeNodeStatus, at time.Time) OfficeNode {
	node.LastCheckedAt, node.Status = &at, status
	if status == nil {
		node.Health = OfficeNodeUnavailable
	} else {
		node.Health = OfficeNodeOnline
		node.WorkerIdentity = status.NodeID
	}
	return node
}

func (s *OfficeAgentService) probeOfficeNodes() {
	if !s.Enabled() {
		return
	}
	ctx, stop := context.WithTimeout(context.Background(), 15*time.Second)
	defer stop()
	nodes, err := s.officeNodes(ctx)
	if err != nil {
		return
	}
	var workers sync.WaitGroup
	batch := make(chan struct{}, 8)
	for _, node := range nodes {
		if node.State == OfficeNodeDisabled || ctx.Err() != nil {
			continue
		}
		batch <- struct{}{}
		workers.Go(func() {
			defer func() { <-batch }()
			_, _ = s.refreshOfficeNodeStatus(ctx, node)
		})
	}
	workers.Wait()
}

func validOfficeNodeToken(token string) bool {
	return len(token) >= 32 && len(token) <= 256 && strings.IndexFunc(token, func(r rune) bool { return r < 33 || r > 126 }) < 0
}

func (s *OfficeAgentService) canStoreOfficeNodeToken() bool {
	return s.officeNodeEncryptor != nil && s.settings != nil && s.settings.cfg != nil && s.settings.IsTotpEncryptionKeyConfigured()
}

func (s *OfficeAgentService) officeNodeCredential(_ context.Context, node OfficeNode) (string, error) {
	if !s.canStoreOfficeNodeToken() {
		return "", ErrOfficeNodeSecret
	}
	plain, err := s.officeNodeEncryptor.Decrypt(node.TokenEncrypted)
	token, valid := strings.CutPrefix(plain, node.ID+":")
	if err != nil || !valid || !validOfficeNodeToken(token) {
		return "", ErrOfficeNodeSecret
	}
	return token, nil
}

func (s *OfficeAgentService) probeOfficeNode(ctx context.Context, node OfficeNode) (OfficeNodeStatus, error) {
	return s.requestOfficeNodeStatus(ctx, node, http.MethodGet, "/v1/status", nil)
}

func (s *OfficeAgentService) applyOfficeNodeConcurrency(ctx context.Context, node OfficeNode) (OfficeNodeStatus, error) {
	body, err := json.Marshal(struct {
		Concurrency int   `json:"concurrency"`
		Revision    int64 `json:"revision"`
	}{Concurrency: node.MaxConcurrent, Revision: node.Revision})
	if err != nil {
		return OfficeNodeStatus{}, err
	}
	status, err := s.requestOfficeNodeStatus(ctx, node, http.MethodPut, "/v1/concurrency", body)
	if err != nil {
		return OfficeNodeStatus{}, err
	}
	if status.ConcurrencyRevision == nil || *status.ConcurrencyRevision < node.Revision {
		return OfficeNodeStatus{}, ErrOfficeNodeUnavailable
	}
	return status, nil
}

func (s *OfficeAgentService) requestOfficeNodeStatus(ctx context.Context, node OfficeNode, method, endpoint string, body []byte) (OfficeNodeStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	code, raw, identity, err := s.sendOfficeNodeRequest(ctx, node, method, endpoint, body, 16<<10)
	if err != nil {
		return OfficeNodeStatus{}, err
	}
	var status OfficeNodeStatus
	if code != http.StatusOK || json.Unmarshal(raw, &status) != nil || !validOfficeNodeStatus(status) {
		return OfficeNodeStatus{}, ErrOfficeNodeUnavailable
	}
	if identity != status.NodeID {
		return OfficeNodeStatus{}, ErrOfficeNodeIdentity
	}
	return status, nil
}

func validOfficeNodeStatus(status OfficeNodeStatus) bool {
	maxRunning := status.Concurrency
	if status.ConcurrencyRevision != nil {
		if *status.ConcurrencyRevision < 0 {
			return false
		}
		// A lower limit applies to admissions while already running jobs finish.
		maxRunning = 8
	}
	if !officeIdentifier(status.NodeID, 128) || len(status.Version) > 80 || len(status.Actions) == 0 || len(status.Actions) > 64 || len(status.StorageOrigins) > 16 ||
		status.Concurrency < 1 || status.Concurrency > 8 || status.Running < 0 || status.Running > maxRunning || status.Queued < 0 || status.Queued > 64 || status.AwaitingUpload < 0 || status.AwaitingUpload > 1024 {
		return false
	}
	for _, action := range status.Actions {
		if !officeIdentifier(action, 64) {
			return false
		}
	}
	for _, origin := range status.StorageOrigins {
		if _, err := normalizeOfficeNodeOrigin(origin); err != nil {
			return false
		}
	}
	return true
}
