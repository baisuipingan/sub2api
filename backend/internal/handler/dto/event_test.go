package dto

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestUserEventOmitsAdminOnlyMetadata(t *testing.T) {
	value := &service.Event{
		ID: 12, Title: "Targeted event", Status: service.EventStatusPublished,
		Visibility:           service.EventVisibilityTargeted,
		Audience:             service.EventAudience{SubscriptionGroupIDs: []int64{7}},
		ManualOverrideFields: []string{"title"},
	}
	encoded, err := json.Marshal(UserEventFromService(value))
	if err != nil {
		t.Fatalf("marshal user event: %v", err)
	}
	payload := string(encoded)
	for _, field := range []string{"visibility", "audience", "manual_override_fields", "visible_from", "visible_until"} {
		if strings.Contains(payload, `"`+field+`"`) {
			t.Fatalf("user event leaked %s: %s", field, payload)
		}
	}
}
