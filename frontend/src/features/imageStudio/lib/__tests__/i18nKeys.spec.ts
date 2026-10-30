import fs from 'node:fs'
import path from 'node:path'
import { beforeAll, describe, expect, it } from 'vitest'
import i18n, { loadLocaleMessages } from '@/i18n'

/**
 * 防止「i18n key 写错 / 文案没被加载」这类静默问题。
 *
 * 关键教训：文案文件里写对了 ≠ 运行时能用。
 * 本项目 messages 是懒加载的（`messages: {}` + 动态 import），
 * 所以必须断言**运行时解析结果**，只比对源文件会漏掉接线错误。
 *
 * vue-i18n 找不到 key 时会把 key 原样显示（界面上出现
 * 「imageStudio.store.catalogFailed」），编译期不报任何错。
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

const i18nGlobal = i18n.global as unknown as {
  te: (key: string, locale?: string) => boolean
  t: (key: string) => string
}

describe('imageStudio i18n 完整性', () => {
  const used = collectUsedKeys()

  beforeAll(async () => {
    // messages 是懒加载的，测试环境需要显式加载才有得比对
    await loadLocaleMessages('zh')
    await loadLocaleMessages('en')
  })

  it('确实扫描到了 key（避免正则失效后测试形同虚设）', () => {
    expect(used.size).toBeGreaterThan(50)
  })

  it('运行时可解析（中文）', () => {
    const missing = [...used].filter((key) => !i18nGlobal.te(key, 'zh'))
    expect(missing).toEqual([])
  })

  it('运行时可解析（英文）', () => {
    const missing = [...used].filter((key) => !i18nGlobal.te(key, 'en'))
    expect(missing).toEqual([])
  })

  it('te() 的判定是有意义的（不存在的 key 必须为 false）', () => {
    // 说明：测试环境把 vue-i18n 换成了 runtime-only 构建（不含消息编译器），
    // 所以 t() 一律返回 key，不能用它判断译文是否生效。
    // 但 te() 是纯存在性检查，不受此影响，足以守住「key 写错/漏配」。
    expect(i18nGlobal.te('imageStudio.lib.maskSizeMismatch', 'zh')).toBe(true)
    expect(i18nGlobal.te('imageStudio.lib.__not_exist__', 'zh')).toBe(false)
  })
})
