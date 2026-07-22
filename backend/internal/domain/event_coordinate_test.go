package domain

import (
	"math"
	"testing"
)

func TestGCJ02ToWGS84(t *testing.T) {
	t.Run("outside mainland China", func(t *testing.T) {
		latitude, longitude := GCJ02ToWGS84(35.6762, 139.6503)
		if latitude != 35.6762 || longitude != 139.6503 {
			t.Fatalf("outside-China coordinate changed: got (%f, %f)", latitude, longitude)
		}
	})

	t.Run("Shanghai", func(t *testing.T) {
		latitude, longitude := GCJ02ToWGS84(31.228457, 121.478223)
		if math.Abs(latitude-31.2304) > 0.0002 || math.Abs(longitude-121.4737) > 0.0002 {
			t.Fatalf("unexpected WGS84 coordinate: got (%f, %f)", latitude, longitude)
		}
	})
}
