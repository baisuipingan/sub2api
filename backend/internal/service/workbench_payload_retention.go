package service

import (
	"context"
	"log/slog"
	"time"
)

const (
	workbenchStaleRunAge  = 24 * time.Hour
	workbenchPayloadGrace = 24 * time.Hour
)

func (s *OfficeAgentService) sweepWorkbenchPayloads() {
	repo, ok := s.repo.(interface {
		SweepWorkbenchPayloads(context.Context, time.Time, time.Time, int) error
	})
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	now := time.Now().UTC()
	if err := repo.SweepWorkbenchPayloads(ctx, now.Add(-workbenchStaleRunAge), now.Add(-workbenchPayloadGrace), 20); err != nil {
		slog.Warn("workbench payload cleanup failed", "error", err)
	}
}
