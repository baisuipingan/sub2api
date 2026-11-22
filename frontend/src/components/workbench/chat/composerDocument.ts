/**
 * Adapted from Bisheng Chat/Input/ComposerEditor.tsx at a8e5be89 (Apache-2.0).
 * Extract the plain draft while retaining line breaks inserted by the browser.
 */
export function readComposerDocument(root: HTMLElement): string {
  const visit = (node: Node): string => {
    if (node.nodeType === Node.TEXT_NODE) return node.textContent || ''
    if (!(node instanceof HTMLElement)) return ''
    if (node.tagName === 'BR') return node.nextSibling ? '\n' : ''
    const content = Array.from(node.childNodes).map(visit).join('')
    return /^(DIV|P)$/.test(node.tagName) && node !== root && node.previousSibling ? `\n${content}` : content
  }
  return visit(root)
}
