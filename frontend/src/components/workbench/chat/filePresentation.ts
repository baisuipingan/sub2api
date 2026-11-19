import type { ChatArtifact } from '@/types/workbenchChat'
export function fileExtension(file: ChatArtifact) { return file.name.split('.').pop()?.toLowerCase() || '' }
export function fileType(file: ChatArtifact) {
  const ext = fileExtension(file)
  if (['doc', 'docx'].includes(ext)) return { label: 'Word', short: 'W', family: 'word' }
  if (['xls', 'xlsx', 'csv'].includes(ext)) return { label: ext === 'csv' ? 'CSV' : 'Excel', short: 'X', family: 'sheet' }
  if (['ppt', 'pptx'].includes(ext)) return { label: 'PowerPoint', short: 'P', family: 'slides' }
  if (ext === 'pdf') return { label: 'PDF', short: 'PDF', family: 'pdf' }
  if (file.mime.startsWith('image/')) return { label: ext.toUpperCase() || '图片', short: 'IMG', family: 'image' }
  if (['md', 'markdown'].includes(ext)) return { label: 'Markdown', short: 'M↓', family: 'text' }
  return { label: ext.toUpperCase() || '文件', short: ext === 'json' ? '{}' : 'TXT', family: 'text' }
}
export function fileSize(bytes: number) {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(bytes < 10 * 1024 ? 1 : 0)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
export function isOutput(file: ChatArtifact) { return file.role ? file.role === 'output' : file.source === 'generated' }
