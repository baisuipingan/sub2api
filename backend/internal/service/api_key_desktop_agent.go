package service

import (
	"context"
	"errors"
	"fmt"
)

var ErrDesktopAgentPolicyChanged = errors.New("desktop agent policy changed")

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
