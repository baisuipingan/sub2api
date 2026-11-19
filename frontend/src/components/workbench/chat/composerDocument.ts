/**
 * Adapted from Bisheng Chat/Input/ComposerEditor.tsx at a8e5be89 (Apache-2.0).
 * The document owns native selection/undo; resource identities never enter the prompt.
 */
export interface ComposerReference {
  key: string
  kind: 'task' | 'tool'
  label: string
}

export const composerReferenceSelector = '[data-composer-reference]'

export function readComposerDocument(root: HTMLElement): { text: string; keys: string[] } {
  const keys: string[] = []
  const visit = (node: Node): string => {
    if (node.nodeType === Node.TEXT_NODE) return node.textContent || ''
    if (!(node instanceof HTMLElement)) return ''
    if (node.dataset.composerReference) {
      keys.push(node.dataset.composerReference)
      return ''
    }
    if (node.tagName === 'BR') return node.nextSibling ? '\n' : ''
    const content = Array.from(node.childNodes).map(visit).join('')
    return /^(DIV|P)$/.test(node.tagName) && node !== root && node.previousSibling ? `\n${content}` : content
  }
  return { text: visit(root), keys }
}

const referenceIconPaths = {
  task: 'M9 6h11M9 12h11M9 18h11M3 5l1 1 2-2M3 11l1 1 2-2M3 17l1 1 2-2',
  tool: 'M12 21a9 9 0 1 0 0-18 9 9 0 0 0 0 18Zm0 0c2.5 0 4.5-4 4.5-9S14.5 3 12 3 7.5 7 7.5 12 9.5 21 12 21ZM3 12h18',
}

/** Labels are text nodes, even when a model/provider supplies an unusual name. */
export function createComposerReference(reference: ComposerReference): HTMLSpanElement {
  const token = document.createElement('span')
  token.dataset.composerReference = reference.key
  token.dataset.referenceKind = reference.kind
  token.setAttribute('contenteditable', 'false')
  token.className = 'bs-composer-inline-reference'
  token.title = `${reference.label} · 选中后按退格键移除`
  const icon = document.createElementNS('http://www.w3.org/2000/svg', 'svg')
  icon.setAttribute('width', '14')
  icon.setAttribute('height', '14')
  icon.setAttribute('viewBox', '0 0 24 24')
  icon.setAttribute('fill', 'none')
  icon.setAttribute('stroke', 'currentColor')
  icon.setAttribute('stroke-width', '1.5')
  icon.setAttribute('aria-hidden', 'true')
  const path = document.createElementNS('http://www.w3.org/2000/svg', 'path')
  path.setAttribute('d', referenceIconPaths[reference.kind])
  icon.append(path)
  const label = document.createElement('span')
  label.className = 'bs-composer-reference-label'
  label.textContent = reference.label
  token.append(icon, label)
  return token
}
