const getMentionTextLength = (element: Element) => element.textContent?.length ?? 0

function getVisibleTextLength(node: Node): number {
  if (node.nodeType === Node.TEXT_NODE) return node.textContent?.length ?? 0
  if (node instanceof HTMLElement && node.classList.contains('mention-tag')) return getMentionTextLength(node)
  return Array.from(node.childNodes).reduce((sum, child) => sum + getVisibleTextLength(child), 0)
}

function getVisibleOffsetBeforeNode(root: HTMLElement, target: Node) {
  let offset = 0
  let found = false

  const walk = (node: Node) => {
    if (found) return
    if (node === target) {
      found = true
      return
    }
    if (node.nodeType === Node.TEXT_NODE) {
      offset += node.textContent?.length ?? 0
      return
    }
    if (node instanceof HTMLElement && node.classList.contains('mention-tag')) {
      offset += getMentionTextLength(node)
      return
    }
    node.childNodes.forEach(walk)
  }

  root.childNodes.forEach(walk)
  return offset
}

function getMentionForBoundary(root: HTMLElement, container: Node) {
  const element = container.nodeType === Node.ELEMENT_NODE ? container as Element : container.parentElement
  const mention = element?.closest('.mention-tag')
  return mention && root.contains(mention) ? mention : null
}

function getOffsetInsideMention(mention: Element, container: Node, offset: number) {
  try {
    const range = document.createRange()
    range.selectNodeContents(mention)
    range.setEnd(container, offset)
    return range.toString().length
  } catch {
    return getMentionTextLength(mention)
  }
}

function getBoundaryOffset(
  root: HTMLElement,
  container: Node,
  offset: number,
  edge: 'start' | 'end',
  collapsed: boolean,
) {
  if (container === root) {
    return Array.from(root.childNodes)
      .slice(0, offset)
      .reduce((sum, child) => sum + getVisibleTextLength(child), 0)
  }

  if (!root.contains(container)) return edge === 'start' ? 0 : root.textContent?.length ?? 0

  const mention = getMentionForBoundary(root, container)
  if (mention) {
    const mentionStart = getVisibleOffsetBeforeNode(root, mention)
    const mentionLength = getMentionTextLength(mention)
    if (!collapsed) return edge === 'start' ? mentionStart : mentionStart + mentionLength
    const mentionOffset = getOffsetInsideMention(mention, container, offset)
    return mentionStart + (mentionOffset < mentionLength / 2 ? 0 : mentionLength)
  }

  if (container.nodeType === Node.TEXT_NODE) return getVisibleOffsetBeforeNode(root, container) + offset

  if (container.nodeType === Node.ELEMENT_NODE) {
    const element = container as Element
    let visibleOffset = element === root ? 0 : getVisibleOffsetBeforeNode(root, element)
    for (const child of Array.from(element.childNodes).slice(0, offset)) {
      visibleOffset += getVisibleTextLength(child)
    }
    return visibleOffset
  }

  return root.textContent?.length ?? 0
}

export function getContentEditableSelection(element: HTMLElement) {
  const selection = window.getSelection()
  if (!selection || selection.rangeCount === 0) {
    const end = element.textContent?.length ?? 0
    return { start: end, end }
  }

  try {
    const range = selection.getRangeAt(0)
    const start = getBoundaryOffset(element, range.startContainer, range.startOffset, 'start', range.collapsed)
    const end = range.collapsed
      ? start
      : getBoundaryOffset(element, range.endContainer, range.endOffset, 'end', false)
    return { start, end }
  } catch {
    const end = element.textContent?.length ?? 0
    return { start: end, end }
  }
}

export function getContentEditablePlainText(element: HTMLElement) {
  let text = ''
  const appendText = (node: Node) => {
    if (node.nodeType === Node.TEXT_NODE) {
      text += node.textContent ?? ''
      return
    }
    if (node instanceof HTMLElement && node.classList.contains('mention-tag')) {
      text += node.dataset.mentionText ?? node.textContent ?? ''
      return
    }
    node.childNodes.forEach(appendText)
  }
  element.childNodes.forEach(appendText)
  return text.replace(/\r\n?/g, '\n')
}

type SelectionBoundary =
  | { type: 'offset'; node: Node; offset: number }
  | { type: 'before'; element: Element }
  | { type: 'after'; element: Element }

function findBoundary(element: HTMLElement, targetOffset: number, edge: 'start' | 'end'): SelectionBoundary {
  let remaining = targetOffset
  let lastBoundary: SelectionBoundary = { type: 'offset', node: element, offset: 0 }

  const walk = (node: Node): SelectionBoundary | null => {
    if (node.nodeType === Node.TEXT_NODE) {
      const textNode = node as Text
      lastBoundary = { type: 'offset', node: textNode, offset: textNode.length }
      if (remaining <= textNode.length) return { type: 'offset', node: textNode, offset: remaining }
      remaining -= textNode.length
      return null
    }

    if (node instanceof HTMLElement && node.classList.contains('mention-tag')) {
      const length = getMentionTextLength(node)
      if (remaining <= 0) return { type: 'before', element: node }
      if (remaining < length) return edge === 'start' ? { type: 'before', element: node } : { type: 'after', element: node }
      if (remaining === length) return { type: 'after', element: node }
      remaining -= length
      return null
    }

    for (const child of Array.from(node.childNodes)) {
      const boundary = walk(child)
      if (boundary) return boundary
    }
    return null
  }

  return walk(element) ?? lastBoundary
}

function applyBoundary(range: Range, boundary: SelectionBoundary, target: 'start' | 'end') {
  if (boundary.type === 'before') {
    target === 'start' ? range.setStartBefore(boundary.element) : range.setEndBefore(boundary.element)
    return
  }
  if (boundary.type === 'after') {
    target === 'start' ? range.setStartAfter(boundary.element) : range.setEndAfter(boundary.element)
    return
  }
  target === 'start'
    ? range.setStart(boundary.node, boundary.offset)
    : range.setEnd(boundary.node, boundary.offset)
}

export function setContentEditableSelection(element: HTMLElement, start: number, end: number) {
  const selection = window.getSelection()
  if (!selection) return

  const range = document.createRange()
  applyBoundary(range, findBoundary(element, start, 'start'), 'start')
  applyBoundary(range, findBoundary(element, end, 'end'), 'end')
  selection.removeAllRanges()
  selection.addRange(range)
}

export function insertContentEditableMention(element: HTMLElement, text: string, mentionText: string) {
  const selection = window.getSelection()
  if (!selection || selection.rangeCount === 0) return false

  const range = selection.getRangeAt(0)
  if (!element.contains(range.commonAncestorContainer)) return false

  const mention = document.createElement('span')
  mention.contentEditable = 'false'
  mention.className = 'mention-tag'
  mention.dataset.mentionText = mentionText
  mention.textContent = text
  range.deleteContents()
  range.insertNode(mention)
  range.setStartAfter(mention)
  range.collapse(true)
  selection.removeAllRanges()
  selection.addRange(range)
  return true
}

export function insertContentEditableText(element: HTMLElement, text: string) {
  const selection = window.getSelection()
  if (!selection || selection.rangeCount === 0) return false

  const range = selection.getRangeAt(0)
  if (!element.contains(range.commonAncestorContainer)) return false

  const textNode = document.createTextNode(text)
  range.deleteContents()
  range.insertNode(textNode)
  range.setStartAfter(textNode)
  range.collapse(true)
  selection.removeAllRanges()
  selection.addRange(range)
  return true
}

export function syncMentionTagSelection(element: HTMLElement) {
  const selection = window.getSelection()
  const range = selection?.rangeCount ? selection.getRangeAt(0) : null
  element.querySelectorAll<HTMLElement>('.mention-tag').forEach((mention) => {
    let selected = false
    try {
      selected = Boolean(range && !range.collapsed && range.intersectsNode(mention))
    } catch {
      selected = false
    }
    mention.classList.toggle('selected', selected)
  })
}
