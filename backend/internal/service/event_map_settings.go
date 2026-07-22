package service

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type EventMapSettings struct {
	Enabled      bool    `json:"enabled"`
	AMapKey      string  `json:"amap_key"`
	SecurityCode string  `json:"security_code"`
	DefaultLat   float64 `json:"default_latitude"`
	DefaultLng   float64 `json:"default_longitude"`
	DefaultZoom  int     `json:"default_zoom"`
}

func (s *SettingService) IsEventCenterEnabled(ctx context.Context) bool {
	if s == nil || s.settingRepo == nil {
		return true
	}
	value, err := s.settingRepo.GetValue(ctx, SettingKeyEventCenterEnabled)
	if err != nil {
		return true
	}
	return !isFalseSettingValue(value)
}

func (s *SettingService) GetEventMapSettings(ctx context.Context) (*EventMapSettings, error) {
	values, err := s.settingRepo.GetMultiple(ctx, []string{
		SettingKeyEventCenterEnabled, SettingKeyEventMapAMapKey, SettingKeyEventMapAMapSecurityCode,
		SettingKeyEventMapDefaultLatitude, SettingKeyEventMapDefaultLongitude, SettingKeyEventMapDefaultZoom,
	})
	if err != nil {
		return nil, fmt.Errorf("get event map settings: %w", err)
	}
	return &EventMapSettings{
		Enabled:      !isFalseSettingValue(values[SettingKeyEventCenterEnabled]),
		AMapKey:      strings.TrimSpace(values[SettingKeyEventMapAMapKey]),
		SecurityCode: strings.TrimSpace(values[SettingKeyEventMapAMapSecurityCode]),
		DefaultLat:   parseEventMapFloat(values[SettingKeyEventMapDefaultLatitude], 31.2304, -90, 90),
		DefaultLng:   parseEventMapFloat(values[SettingKeyEventMapDefaultLongitude], 121.4737, -180, 180),
		DefaultZoom:  parseEventMapInt(values[SettingKeyEventMapDefaultZoom], 11, 3, 20),
	}, nil
}

func (s *SettingService) SetEventMapSettings(ctx context.Context, settings EventMapSettings) error {
	if settings.DefaultLat < -90 || settings.DefaultLat > 90 || settings.DefaultLng < -180 || settings.DefaultLng > 180 || settings.DefaultZoom < 3 || settings.DefaultZoom > 20 {
		return infraerrors.BadRequest("EVENT_MAP_SETTINGS_INVALID", "invalid event map settings")
	}
	updates := map[string]string{
		SettingKeyEventCenterEnabled:       strconv.FormatBool(settings.Enabled),
		SettingKeyEventMapAMapKey:          strings.TrimSpace(settings.AMapKey),
		SettingKeyEventMapAMapSecurityCode: strings.TrimSpace(settings.SecurityCode),
		SettingKeyEventMapDefaultLatitude:  strconv.FormatFloat(settings.DefaultLat, 'f', 6, 64),
		SettingKeyEventMapDefaultLongitude: strconv.FormatFloat(settings.DefaultLng, 'f', 6, 64),
		SettingKeyEventMapDefaultZoom:      strconv.Itoa(settings.DefaultZoom),
	}
	if err := s.settingRepo.SetMultiple(ctx, updates); err != nil {
		return fmt.Errorf("update event map settings: %w", err)
	}
	if s.onUpdate != nil {
		s.onUpdate()
	}
	return nil
}

func parseEventMapFloat(raw string, fallback, minValue, maxValue float64) float64 {
	value, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || value < minValue || value > maxValue {
		return fallback
	}
	return value
}

func parseEventMapInt(raw string, fallback, minValue, maxValue int) int {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < minValue || value > maxValue {
		return fallback
	}
	return value
}
