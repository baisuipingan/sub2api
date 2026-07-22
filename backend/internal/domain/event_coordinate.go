package domain

import "math"

const (
	eventCoordinateA  = 6378245.0
	eventCoordinateEE = 0.00669342162296594323
)

// GCJ02ToWGS84 converts mainland China GCJ-02 coordinates to the canonical
// WGS84 representation stored by the event domain. The inverse-offset method
// is sufficiently accurate for venue markers while avoiding provider lock-in.
func GCJ02ToWGS84(latitude, longitude float64) (float64, float64) {
	if outsideMainlandChina(latitude, longitude) {
		return latitude, longitude
	}
	dLat, dLng := gcj02Delta(latitude, longitude)
	return latitude - dLat, longitude - dLng
}

func gcj02Delta(latitude, longitude float64) (float64, float64) {
	dLat := transformLatitude(longitude-105, latitude-35)
	dLng := transformLongitude(longitude-105, latitude-35)
	radLat := latitude / 180 * math.Pi
	magic := math.Sin(radLat)
	magic = 1 - eventCoordinateEE*magic*magic
	sqrtMagic := math.Sqrt(magic)
	dLat = (dLat * 180) / ((eventCoordinateA * (1 - eventCoordinateEE) / (magic * sqrtMagic)) * math.Pi)
	dLng = (dLng * 180) / (eventCoordinateA / sqrtMagic * math.Cos(radLat) * math.Pi)
	return dLat, dLng
}

func transformLatitude(x, y float64) float64 {
	ret := -100 + 2*x + 3*y + 0.2*y*y + 0.1*x*y + 0.2*math.Sqrt(math.Abs(x))
	ret += (20*math.Sin(6*x*math.Pi) + 20*math.Sin(2*x*math.Pi)) * 2 / 3
	ret += (20*math.Sin(y*math.Pi) + 40*math.Sin(y/3*math.Pi)) * 2 / 3
	ret += (160*math.Sin(y/12*math.Pi) + 320*math.Sin(y*math.Pi/30)) * 2 / 3
	return ret
}

func transformLongitude(x, y float64) float64 {
	ret := 300 + x + 2*y + 0.1*x*x + 0.1*x*y + 0.1*math.Sqrt(math.Abs(x))
	ret += (20*math.Sin(6*x*math.Pi) + 20*math.Sin(2*x*math.Pi)) * 2 / 3
	ret += (20*math.Sin(x*math.Pi) + 40*math.Sin(x/3*math.Pi)) * 2 / 3
	ret += (150*math.Sin(x/12*math.Pi) + 300*math.Sin(x/30*math.Pi)) * 2 / 3
	return ret
}

func outsideMainlandChina(latitude, longitude float64) bool {
	return longitude < 72.004 || longitude > 137.8347 || latitude < 0.8293 || latitude > 55.8271
}
