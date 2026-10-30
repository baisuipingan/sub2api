import type { TaskParams, TaskRecord } from '../types'

export function createTaskDonePatch(task: Pick<TaskRecord, 'createdAt'>, now: number) {
  return {
    status: 'done' as const,
    error: null,
    finishedAt: now,
    elapsed: Math.max(0, now - task.createdAt),
  }
}

export function createTaskErrorPatch(task: Pick<TaskRecord, 'createdAt'>, error: string, now: number) {
  return {
    status: 'error' as const,
    error,
    finishedAt: now,
    elapsed: Math.max(0, now - task.createdAt),
  }
}

export function firstActualParams(paramsList: Array<Partial<TaskParams> | undefined> | undefined) {
  return paramsList?.find((params) => params && Object.keys(params).length > 0)
}

export function mapActualParamsByImage(outputIds: string[], paramsList: Array<Partial<TaskParams> | undefined> | undefined) {
  const mapped: Record<string, Partial<TaskParams>> = {}
  for (const [index, params] of (paramsList ?? []).entries()) {
    const imageId = outputIds[index]
    if (imageId && params && Object.keys(params).length) mapped[imageId] = params
  }
  return Object.keys(mapped).length ? mapped : undefined
}

export function addImageSizeParam(params: Partial<TaskParams> | undefined, size: { width?: number; height?: number } | undefined) {
  if (!size?.width || !size.height) return params
  return { ...(params ?? {}), size: `${size.width}x${size.height}` }
}

export function mapRevisedPromptsByImage(outputIds: string[], revisedPrompts: Array<string | undefined> | undefined) {
  const mapped: Record<string, string> = {}
  for (const [index, prompt] of (revisedPrompts ?? []).entries()) {
    const imageId = outputIds[index]
    if (imageId && prompt?.trim()) mapped[imageId] = prompt.trim()
  }
  return Object.keys(mapped).length ? mapped : undefined
}
