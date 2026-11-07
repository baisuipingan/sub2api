export function formatExportFileTime(date: Date): string {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}_${pad(date.getHours())}-${pad(date.getMinutes())}-${pad(date.getSeconds())}`
}

export function getNumberedFileNameBase(fileNameBase: string, index: number, total: number) {
  return total > 1 ? `${fileNameBase}-${String(index + 1).padStart(2, '0')}` : fileNameBase
}

export function sanitizeFileNamePart(value: string): string {
  return value.trim().replace(/[<>:"/\\|?*\p{Cc}]+/gu, '-').replace(/\s+/g, ' ').slice(0, 120)
}
