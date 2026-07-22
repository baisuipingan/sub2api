package migrations

import (
	"strings"
	"testing"
)

func TestEventCenterFoundationMigrationContainsCoreConstraints(t *testing.T) {
	content, err := FS.ReadFile("182_event_center_foundation.sql")
	if err != nil {
		t.Fatalf("read event center migration: %v", err)
	}
	sql := string(content)
	required := []string{
		"CREATE TABLE IF NOT EXISTS events",
		"CREATE TABLE IF NOT EXISTS event_occurrences",
		"CREATE TABLE IF NOT EXISTS event_sources",
		"CREATE TABLE IF NOT EXISTS event_source_records",
		"CREATE TABLE IF NOT EXISTS event_import_batches",
		"CREATE TABLE IF NOT EXISTS event_import_items",
		"events_status_check",
		"event_occurrences_coordinate_pair_check",
		"event_source_records_external_uq",
		"event_import_items_batch_index_uq",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
}

func TestEventCenterHardeningMigrationContainsIdempotencyAndAudienceIndexes(t *testing.T) {
	content, err := FS.ReadFile("183_event_center_hardening.sql")
	if err != nil {
		t.Fatalf("read event center hardening migration: %v", err)
	}
	sql := string(content)
	required := []string{
		"event_source_records_source_fingerprint_uq",
		"ON event_source_records (source_id, fingerprint)",
		"events_audience_gin_idx",
		"USING GIN (audience jsonb_path_ops)",
	}
	for _, fragment := range required {
		if !strings.Contains(sql, fragment) {
			t.Errorf("migration missing %q", fragment)
		}
	}
}
