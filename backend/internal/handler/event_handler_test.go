package handler

import "testing"

func TestParseEventMapBounds(t *testing.T) {
	valid, ok := parseEventMapBounds("120.1,30.1,122.2,32.2")
	if !ok || valid == nil || valid.MinLongitude != 120.1 || valid.MaxLatitude != 32.2 {
		t.Fatalf("valid bounds rejected: %#v, %v", valid, ok)
	}
	for _, value := range []string{
		"120,30,122", "x,30,122,32", "122,30,120,32", "120,32,122,30", "-181,30,122,32", "120,30,181,32",
	} {
		if _, ok := parseEventMapBounds(value); ok {
			t.Fatalf("invalid bounds accepted: %q", value)
		}
	}
}
