import { libT } from './i18nMessage'
import { strFromU8, strToU8, unzip, zip } from 'fflate'
import type { AppSettings, ExportData, FavoriteCollection, StoredImage, StoredImageThumbnail, TaskRecord } from '../types'
import { bytesToDataUrl, dataUrlToBytes } from './dataUrl'
import { getNumberedFileNameBase, sanitizeFileNamePart } from './exportFileName'
import { getDataUrlDecodedByteSize } from './imageApiShared'

type ZipFiles = Record<string, Uint8Array | [Uint8Array, { mtime: Date; level?: 0 }]>

export const MAX_EXPORT_ZIP_BYTES = 2 * 1024 * 1024 * 1024
const DEFAULT_EXPORT_PART_BYTES = 256 * 1024 * 1024
const ZIP_ENTRY_OVERHEAD_BYTES = 1024

export interface BuildExportZipOptions {
  exportConfig?: boolean
  exportTasks?: boolean
}

export interface BuildExportZipParams {
  options: BuildExportZipOptions
  exportedAt: number
  settings: AppSettings
  tasks: TaskRecord[]
  images: StoredImage[]
  thumbnailsByImageId: Map<string, StoredImageThumbnail>
  favoriteCollections: FavoriteCollection[]
  defaultFavoriteCollectionId: string | null
  includeManifestData?: boolean
  backupPart?: ExportData['backupPart']
}

export interface ExportZipContents {
  manifest: ExportData
  files: Record<string, Uint8Array>
}

export interface ExportImageSize {
  id: string
  bytes: number
}

export interface ExportZipPlanPart {
  imageIds: string[]
  tasks: TaskRecord[]
  includeBaseData: boolean
}

export async function buildExportZip(params: BuildExportZipParams) {
  const exportedAtDate = new Date(params.exportedAt)
  const fileBases = getImageFileNameBases(params.tasks)
  const imageFiles: NonNullable<ExportData['imageFiles']> = {}
  const thumbnailFiles: NonNullable<ExportData['thumbnailFiles']> = {}
  const files: ZipFiles = {}
  const usedPaths = new Set<string>()

  if (params.options.exportTasks) {
    for (const image of params.images) {
      const { ext, bytes } = dataUrlToBytes(image.dataUrl)
      const path = getUniqueImagePath(fileBases.get(image.id) ?? `image-${image.id.slice(0, 8)}`, ext, usedPaths)
      const pathBase = path.slice('images/'.length, -(ext.length + 1))
      imageFiles[image.id] = {
        path,
        createdAt: image.createdAt ?? params.exportedAt,
        source: image.source,
        width: image.width,
        height: image.height,
      }
      files[path] = [bytes, { mtime: new Date(imageFiles[image.id].createdAt ?? params.exportedAt), level: 0 }]

      const thumbnail = params.thumbnailsByImageId.get(image.id)
      if (thumbnail?.thumbnailDataUrl) {
        const thumbnailBytes = dataUrlToBytes(thumbnail.thumbnailDataUrl)
        const thumbnailPath = `thumbnails/${pathBase}.${thumbnailBytes.ext}`
        thumbnailFiles[image.id] = {
          path: thumbnailPath,
          width: thumbnail.width,
          height: thumbnail.height,
          thumbnailVersion: thumbnail.thumbnailVersion,
        }
        files[thumbnailPath] = [thumbnailBytes.bytes, { mtime: new Date(imageFiles[image.id].createdAt ?? params.exportedAt), level: 0 }]
      }
    }
  }

  const manifest: ExportData = {
    version: 1,
    exportedAt: exportedAtDate.toISOString(),
    ...(params.backupPart ? { backupPart: params.backupPart } : {}),
    ...(params.options.exportConfig && params.includeManifestData !== false ? { settings: params.settings } : {}),
    ...(params.options.exportTasks ? {
      ...(params.includeManifestData !== false || params.tasks.length ? { tasks: params.tasks } : {}),
      ...(params.includeManifestData !== false ? {
        favoriteCollections: params.favoriteCollections,
        defaultFavoriteCollectionId: params.defaultFavoriteCollectionId,
      } : {}),
      imageFiles,
      thumbnailFiles,
    } : {}),
  }

  files['manifest.json'] = [strToU8(JSON.stringify(manifest, null, 2)), { mtime: exportedAtDate }]
  const bytes = await new Promise<Uint8Array>((resolve, reject) => {
    zip(files, { level: 6 }, (error, value) => error ? reject(error) : resolve(value))
  })
  return { manifest, bytes }
}

export function getExportZipPlan(
  params: Omit<BuildExportZipParams, 'images' | 'thumbnailsByImageId'>,
  images: ExportImageSize[],
  options: { maxBytes?: number; partBytes?: number } = {},
) {
  const maxBytes = options.maxBytes ?? MAX_EXPORT_ZIP_BYTES
  const partBytes = Math.min(options.partBytes ?? DEFAULT_EXPORT_PART_BYTES, maxBytes)
  const tasks = params.options.exportTasks ? params.tasks : []
  const imageList = params.options.exportTasks ? images : []
  const baseBytes = getBaseManifestEstimatedBytes(params)
  const taskBytes = tasks.map((task) => getJsonEstimatedBytes(task))
  const imageBytes = imageList.map((image) => image.bytes + ZIP_ENTRY_OVERHEAD_BYTES)
  const totalBytes = baseBytes + taskBytes.reduce((sum, value) => sum + value, 0) + imageBytes.reduce((sum, value) => sum + value, 0)
  if (totalBytes < partBytes) {
    return [{ imageIds: imageList.map((image) => image.id), tasks, includeBaseData: true }]
  }

  const parts: ExportZipPlanPart[] = [{ imageIds: [], tasks: [], includeBaseData: true }]
  const sizes = [baseBytes]
  const add = (bytes: number, append: (part: ExportZipPlanPart) => void) => {
    let index = parts.length - 1
    if (sizes[index] + bytes >= partBytes && (parts[index].tasks.length || parts[index].imageIds.length)) {
      parts.push({ imageIds: [], tasks: [], includeBaseData: false })
      sizes.push(0)
      index += 1
    }
    if (sizes[index] + bytes >= maxBytes) throw new Error(libT('imageStudio.lib.backupShardTooLarge'))
    append(parts[index])
    sizes[index] += bytes
  }

  tasks.forEach((task, index) => add(taskBytes[index], (part) => part.tasks.push(task)))
  imageList.forEach((image, index) => add(imageBytes[index], (part) => part.imageIds.push(image.id)))
  return parts
}

export function createExportBlob(bytes: Uint8Array) {
  if (bytes.byteLength >= MAX_EXPORT_ZIP_BYTES) throw new Error(libT('imageStudio.lib.backupTooLarge'))
  const buffer = new ArrayBuffer(bytes.byteLength)
  new Uint8Array(buffer).set(bytes)
  return new Blob([buffer], { type: 'application/zip' })
}

export async function readExportZipManifest(bytes: Uint8Array, validateFiles = true) {
  const contents = await readZipFiles(bytes, 'manifest.json')
  const manifest = parseManifest(contents['manifest.json'])
  if (validateFiles) assertExportZipFiles(manifest, (path) => contents[path] != null)
  return manifest
}

export async function readExportZip(bytes: Uint8Array): Promise<ExportZipContents> {
  const files = await readZipFiles(bytes)
  const manifest = parseManifest(files['manifest.json'])
  assertExportZipFiles(manifest, (path) => files[path] != null)
  return { manifest, files }
}

export function getExportImageEstimatedBytes(image: StoredImage, thumbnail?: StoredImageThumbnail) {
  return getDataUrlDecodedByteSize(image.dataUrl)
    + (thumbnail?.thumbnailDataUrl ? getDataUrlDecodedByteSize(thumbnail.thumbnailDataUrl) : 0)
    + ZIP_ENTRY_OVERHEAD_BYTES * (thumbnail?.thumbnailDataUrl ? 2 : 1)
}

export function readExportZipFileAsDataUrl(files: Record<string, Uint8Array>, path: string) {
  const bytes = files[path]
  return bytes ? bytesToDataUrl(bytes, path) : null
}

function readZipFiles(bytes: Uint8Array, only?: string) {
  return new Promise<Record<string, Uint8Array>>((resolve, reject) => {
    unzip(bytes, (error, files) => {
      if (error) {
        reject(error)
        return
      }
      if (!only) {
        resolve(files)
        return
      }
      resolve(files[only] ? { [only]: files[only] } : {})
    })
  })
}

function parseManifest(value: Uint8Array | undefined): ExportData {
  if (!value) throw new Error(libT('imageStudio.lib.backupMissingManifest'))
  const parsed = JSON.parse(strFromU8(value)) as ExportData
  if (!parsed || typeof parsed !== 'object' || typeof parsed.version !== 'number') throw new Error(libT('imageStudio.lib.backupFormatUnknown'))
  return parsed
}

function assertExportZipFiles(manifest: ExportData, hasFile: (path: string) => boolean) {
  const paths = [
    ...Object.values(manifest.imageFiles ?? {}).map((file) => file.path),
    ...Object.values(manifest.thumbnailFiles ?? {}).map((file) => file.path),
  ]
  const missing = paths.find((path) => !hasFile(path))
  if (missing) throw new Error(`备份中缺少 ${missing}`)
}

function getBaseManifestEstimatedBytes(params: Omit<BuildExportZipParams, 'images' | 'thumbnailsByImageId'>) {
  const manifest = {
    version: 1,
    exportedAt: new Date(params.exportedAt).toISOString(),
    ...(params.options.exportConfig ? { settings: params.settings } : {}),
    ...(params.options.exportTasks ? {
      tasks: [],
      favoriteCollections: params.favoriteCollections,
      defaultFavoriteCollectionId: params.defaultFavoriteCollectionId,
    } : {}),
  }
  return strToU8(JSON.stringify(manifest)).byteLength + ZIP_ENTRY_OVERHEAD_BYTES
}

function getJsonEstimatedBytes(value: unknown) {
  return strToU8(JSON.stringify(value)).byteLength + ZIP_ENTRY_OVERHEAD_BYTES
}

function getImageFileNameBases(tasks: TaskRecord[]) {
  const bases = new Map<string, string>()
  for (const task of tasks) {
    addImageFileNameBases(bases, task.outputImages, `task-${task.id}`)
    addImageFileNameBases(bases, task.inputImageIds, `task-${task.id}-input`)
    if (task.maskImageId && !bases.has(task.maskImageId)) bases.set(task.maskImageId, `task-${task.id}-mask`)
  }
  return bases
}

function addImageFileNameBases(bases: Map<string, string>, ids: string[], base: string) {
  const filtered = ids.filter(Boolean)
  for (const [index, id] of filtered.entries()) {
    if (!bases.has(id)) bases.set(id, getNumberedFileNameBase(base, index, filtered.length))
  }
}

function getUniqueImagePath(base: string, ext: string, usedPaths: Set<string>) {
  const safeBase = sanitizeFileNamePart(base) || 'image'
  let path = `images/${safeBase}.${ext}`
  let index = 2
  while (usedPaths.has(path)) {
    path = `images/${safeBase}-${String(index).padStart(2, '0')}.${ext}`
    index += 1
  }
  usedPaths.add(path)
  return path
}
