import fs from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'
import zhMessages from '@/i18n/locales/zh/imageStudio'
import enMessages from '@/i18n/locales/en/imageStudio'

/**
 * 防止「i18n key 写错」这类静默问题。
 *
 * vue-i18n 找不到 key 时会把 key 原样显示给用户（界面上出现
 * 「imageStudio.store.catalogFailed」这种字符串），编译期不会报错。
 * 这里扫描代码里实际引用的 key，断言中英文都齐全。
 */

const FEATURE_ROOT = path.resolve(__dirname, '../..')

function walk(dir: string, out: string[] = []): string[] {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.name === '__tests__' || entry.name === 'node_modules') continue
    const full = path.join(dir, entry.name)
    if (entry.isDirectory()) walk(full, out)
    else if (/\.(ts|vue)$/.test(entry.name)) out.push(full)
  }
  return out
}

function collectUsedKeys(): Set<string> {
  const keys = new Set<string>()
  // t('imageStudio.xxx) / st('imageStudio.xxx') / libT('imageStudio.xxx')
  const pattern = /\b(?:t|st|libT)\(\s*['"`](imageStudio\.[A-Za-z0-9_.]+)['"`]/g

  for (const file of walk(FEATURE_ROOT)) {
    const source = fs.readFileSync(file, 'utf8')
    for (const match of source.matchAll(pattern)) {
      // 动态拼接（含 ${）的 key 无法静态校验，跳过
      if (match[1].includes('${')) continue
      keys.add(match[1])
    }
  }
  return keys
}

function hasKey(messages: Record<string, unknown>, key: string): boolean {
  return key.split('.').reduce<unknown>(
    (node, part) => (node && typeof node === 'object' ? (node as Record<string, unknown>)[part] : undefined),
    messages,
  ) !== undefined
}

describe('imageStudio i18n 完整性', () => {
  const used = collectUsedKeys()

  it('确实扫描到了 key（避免正则失效后测试形同虚设）', () => {
    expect(used.size).toBeGreaterThan(50)
  })

  it('中文文案覆盖所有引用的 key', () => {
    const missing = [...used].filter((key) => !hasKey(zhMessages as never, key))
    expect(missing).toEqual([])
  })

  it('英文文案覆盖所有引用的 key', () => {
    const missing = [...used].filter((key) => !hasKey(enMessages as never, key))
    expect(missing).toEqual([])
  })
})
