const A = 6378245.0
const EE = 0.006693421622965943

export function wgs84ToGcj02(latitude: number, longitude: number): [number, number] {
  if (outsideChina(latitude, longitude)) return [latitude, longitude]
  const [dLat, dLng] = delta(latitude, longitude)
  return [latitude + dLat, longitude + dLng]
}

export function gcj02ToWgs84(latitude: number, longitude: number): [number, number] {
  if (outsideChina(latitude, longitude)) return [latitude, longitude]
  const [dLat, dLng] = delta(latitude, longitude)
  return [latitude - dLat, longitude - dLng]
}

function delta(latitude: number, longitude: number): [number, number] {
  let dLat = transformLatitude(longitude - 105, latitude - 35)
  let dLng = transformLongitude(longitude - 105, latitude - 35)
  const radLat = latitude / 180 * Math.PI
  let magic = Math.sin(radLat)
  magic = 1 - EE * magic * magic
  const sqrtMagic = Math.sqrt(magic)
  dLat = dLat * 180 / (A * (1 - EE) / (magic * sqrtMagic) * Math.PI)
  dLng = dLng * 180 / (A / sqrtMagic * Math.cos(radLat) * Math.PI)
  return [dLat, dLng]
}

function transformLatitude(x: number, y: number): number {
  let result = -100 + 2 * x + 3 * y + 0.2 * y * y + 0.1 * x * y + 0.2 * Math.sqrt(Math.abs(x))
  result += (20 * Math.sin(6 * x * Math.PI) + 20 * Math.sin(2 * x * Math.PI)) * 2 / 3
  result += (20 * Math.sin(y * Math.PI) + 40 * Math.sin(y / 3 * Math.PI)) * 2 / 3
  result += (160 * Math.sin(y / 12 * Math.PI) + 320 * Math.sin(y * Math.PI / 30)) * 2 / 3
  return result
}

function transformLongitude(x: number, y: number): number {
  let result = 300 + x + 2 * y + 0.1 * x * x + 0.1 * x * y + 0.1 * Math.sqrt(Math.abs(x))
  result += (20 * Math.sin(6 * x * Math.PI) + 20 * Math.sin(2 * x * Math.PI)) * 2 / 3
  result += (20 * Math.sin(x * Math.PI) + 40 * Math.sin(x / 3 * Math.PI)) * 2 / 3
  result += (150 * Math.sin(x / 12 * Math.PI) + 300 * Math.sin(x / 30 * Math.PI)) * 2 / 3
  return result
}

function outsideChina(latitude: number, longitude: number): boolean {
  return longitude < 72.004 || longitude > 137.8347 || latitude < 0.8293 || latitude > 55.8271
}
