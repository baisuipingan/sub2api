import type { InputImage } from '../types'

const MENTION_START = '\u2063'
const MENTION_END = '\u2064'
const SELECTED_IMAGE_MENTION_RE = /\u2063@图(\d+)\u2064/g

export interface AtImageQuery {
  start: number
  query: string
}

export type PromptMentionPart =
  | { type: 'text'; text: string }
  | { type: 'mention'; text: string; imageIndex: number }

export function getImageMentionLabel(index: number) {
  return `@图${index + 1}`
}

export function getSelectedImageMentionLabel(index: number) {
  return `${MENTION_START}${getImageMentionLabel(index)}${MENTION_END}`
}

export function stripImageMentionMarkers(prompt: string) {
  return prompt.replace(/[\u2063\u2064]/g, '')
}

export function getPromptIndexFromVisibleIndex(prompt: string, visibleIndex: number) {
  let visible = 0
  for (let index = 0; index < prompt.length; index += 1) {
    if (prompt[index] === MENTION_START || prompt[index] === MENTION_END) continue
    if (visible >= visibleIndex) return index
    visible += 1
  }
  return prompt.length
}

export function isCursorInSelectedImageMention(prompt: string, visibleCursor: number) {
  for (const match of prompt.matchAll(SELECTED_IMAGE_MENTION_RE)) {
    if (match.index == null) continue
    const visibleStart = stripImageMentionMarkers(prompt.slice(0, match.index)).length
    const visibleEnd = visibleStart + getImageMentionLabel(Number(match[1]) - 1).length
    if (visibleCursor > visibleStart && visibleCursor <= visibleEnd) return true
  }
  return false
}

export function getAtImageQuery(prompt: string, cursor: number, imageCount: number): AtImageQuery | null {
  if (imageCount === 0) return null

  const beforeCursor = prompt.slice(0, cursor)
  const atIndex = beforeCursor.lastIndexOf('@')
  if (atIndex < 0) return null

  const query = beforeCursor.slice(atIndex + 1)
  if (/\s/.test(query)) return null
  return { start: atIndex, query }
}

export function imageMentionMatches(query: string, index: number) {
  const normalized = query.trim().toLowerCase()
  if (!normalized) return true

  const oneBasedIndex = String(index + 1)
  return oneBasedIndex.includes(normalized) || `图${oneBasedIndex}`.includes(normalized)
}

export function insertImageMentionAtVisibleRange(prompt: string, start: number, cursor: number, imageIndex: number) {
  const promptStart = getPromptIndexFromVisibleIndex(prompt, start)
  const promptCursor = getPromptIndexFromVisibleIndex(prompt, cursor)
  return {
    prompt: `${prompt.slice(0, promptStart)}${getSelectedImageMentionLabel(imageIndex)}${prompt.slice(promptCursor)}`,
    cursor: start + getImageMentionLabel(imageIndex).length,
  }
}

export function remapImageMentionsForOrder(
  prompt: string,
  previousImages: Array<Pick<InputImage, 'id'>>,
  nextImages: Array<Pick<InputImage, 'id'>>,
  equivalentImageIds: Record<string, string> = {},
) {
  return prompt.replace(SELECTED_IMAGE_MENTION_RE, (text, value) => {
    const previousImage = previousImages[Number(value) - 1]
    if (!previousImage) return stripImageMentionMarkers(text)

    const nextImageId = equivalentImageIds[previousImage.id] ?? previousImage.id
    const nextIndex = nextImages.findIndex((image) => image.id === nextImageId)
    return nextIndex >= 0 ? getSelectedImageMentionLabel(nextIndex) : '@已移除图片'
  })
}

export function getPromptMentionParts(prompt: string, inputImages: InputImage[]): PromptMentionPart[] {
  const parts: PromptMentionPart[] = []
  let lastIndex = 0

  for (const match of prompt.matchAll(SELECTED_IMAGE_MENTION_RE)) {
    if (match.index == null) continue
    const imageIndex = Number(match[1]) - 1
    if (match.index > lastIndex) {
      parts.push({ type: 'text', text: stripImageMentionMarkers(prompt.slice(lastIndex, match.index)) })
    }
    parts.push(inputImages[imageIndex]
      ? { type: 'mention', text: getImageMentionLabel(imageIndex), imageIndex }
      : { type: 'text', text: stripImageMentionMarkers(match[0]) })
    lastIndex = match.index + match[0].length
  }

  if (lastIndex < prompt.length) {
    parts.push({ type: 'text', text: stripImageMentionMarkers(prompt.slice(lastIndex)) })
  }

  return parts.length ? parts : [{ type: 'text', text: stripImageMentionMarkers(prompt) }]
}

export function replaceImageMentionsForApi(prompt: string, imageCount?: number) {
  return prompt.replace(SELECTED_IMAGE_MENTION_RE, (text, value) => {
    const imageIndex = Number(value) - 1
    if (imageCount != null && (imageIndex < 0 || imageIndex >= imageCount)) {
      return stripImageMentionMarkers(text)
    }
    return `[image ${imageIndex + 1}]`
  })
}
