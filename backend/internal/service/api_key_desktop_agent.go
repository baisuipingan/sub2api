package service

import (
	"context"
	"errors"
	"fmt"
	"time"
)

var ErrDesktopAgentPolicyChanged = errors.New("desktop agent policy changed")
var ErrDesktopAgentRestoreAdminRequired = errors.New("disabled desktop credential requires administrator restoration")

// DesktopAgentCredentialCaller is populated only from authenticated server
// context, never decoded from an action body. The repository rechecks the
// caller's current account/role while holding the same transaction locks as the
// credential mutation. StepUpAuthorized means the existing site step-up gate
// allowed this request, following the site's global step-up setting.
type DesktopAgentCredentialCaller struct {
	UserID           int64
	Administrator    bool
	StepUpAuthorized bool
}

func (c DesktopAgentCredentialCaller) Validate(targetUserID int64, reset bool) error {
	if c.UserID <= 0 || (!c.Administrator && c.UserID != targetUserID) || (c.Administrator && reset && !c.StepUpAuthorized) {
		return ErrInsufficientPerms
	}
	return nil
}

// DesktopAgentCredentialMutation deliberately contains no secret. The ordinary
// bootstrap remains the only place that delivers a user's managed credential.
type DesktopAgentCredentialMutation struct {
	Status string `json:"status"`
	KeyID  int64  `json:"key_id"`
}

type desktopAgentCredentialMutator interface {
	MutateDesktopAgentCredential(context.Context, DesktopAgentCredentialCaller, int64, int64, string) (oldKey, newKey string, err error)
}

// ManageDesktopAgentCredential performs an explicit user/admin action. A disabled
// purpose row is retained so a later automatic bootstrap cannot resurrect it.
func (s *APIKeyService) ManageDesktopAgentCredential(ctx context.Context, caller DesktopAgentCredentialCaller, userID, keyID int64, reset bool) (*DesktopAgentCredentialMutation, error) {
	if userID <= 0 || keyID <= 0 {
		return nil, ErrDesktopAgentInvalid
	}
	if err := caller.Validate(userID, reset); err != nil {
		return nil, err
	}
	repo, ok := s.apiKeyRepo.(desktopAgentCredentialMutator)
	if !ok {
		return nil, errors.New("managed credential mutation unavailable")
	}
	replacement := ""
	if reset {
		var err error
		replacement, err = s.GenerateKey()
		if err != nil {
			return nil, err
		}
	}
	oldKey, newKey, err := repo.MutateDesktopAgentCredential(ctx, caller, userID, keyID, replacement)
	if err != nil {
		return nil, err
	}
	// 原因：客户端可能在事务提交后断开；认证失效不能沿用已经取消的HTTP上下文。
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	s.InvalidateAuthCacheByKey(cleanup, oldKey)
	if newKey != "" && newKey != oldKey {
		s.InvalidateAuthCacheByKey(cleanup, newKey)
	}
	status := "revoked"
	if reset {
		status = "active"
	}
	return &DesktopAgentCredentialMutation{Status: status, KeyID: keyID}, nil
}

type desktopAgentKeyRepository interface {
	EnsureDesktopAgentKey(context.Context, *APIKey) (*APIKey, bool, error)
}

// GetDesktopAgentCredential 是独立用途唯一领取入口，不复用或修改图片工作台 Key。
func (s *APIKeyService) GetDesktopAgentCredential(ctx context.Context, userID, groupID int64) (*APIKey, string, error) {
	repo, ok := s.apiKeyRepo.(desktopAgentKeyRepository)
	if !ok {
		return nil, "", errors.New("managed credential repository unavailable")
	}
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, "", err
	}
	if user == nil || user.ID != userID || !user.IsActive() || user.DeletedAt != nil {
		return nil, "", ErrInsufficientPerms
	}
	group, err := s.groupRepo.GetByID(ctx, groupID)
	if err != nil {
		return nil, "", err
	}
	if group == nil || group.ID != groupID || group.Status != StatusActive || group.SubscriptionType != SubscriptionTypeStandard || (group.Platform != PlatformOpenAI && group.Platform != PlatformComposite) || !user.CanBindGroup(group.ID, group.IsExclusive) {
		return nil, "", ErrGroupNotAllowed
	}
	generated, err := s.GenerateKey()
	if err != nil {
		return nil, "", err
	}
	key, changed, err := repo.EnsureDesktopAgentKey(ctx, &APIKey{UserID: userID, Key: generated, Name: "Desktop Agent (managed)", GroupID: &groupID, KeyType: APIKeyTypeSystem, Purpose: APIKeyPurposeDesktopAgent})
	if err != nil {
		return nil, "", fmt.Errorf("ensure desktop agent credential: %w", err)
	}
	if key == nil || key.ID <= 0 || key.Key == "" || key.UserID != userID || key.KeyType != APIKeyTypeSystem || key.Purpose != APIKeyPurposeDesktopAgent {
		return nil, "", errors.New("invalid managed credential identity")
	}
	// 只在事务成功后失效，包括新建时可能存在的认证负缓存；不回写 quota/status/expiry。
	if changed {
		s.InvalidateAuthCacheByKey(ctx, key.Key)
	}
	return key, APIKeyPurposeDesktopAgent, nil
}
