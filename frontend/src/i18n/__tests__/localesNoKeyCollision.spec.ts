import fs from 'node:fs'
import path from 'node:path'
import { describe, expect, it } from 'vitest'

import enAdminAccounts from '../locales/en/admin/accounts'
import enAdminChannels from '../locales/en/admin/channels'
import enAdminOps from '../locales/en/admin/ops'
import enAdminOverview from '../locales/en/admin/overview'
import enAdminResources from '../locales/en/admin/resources'
import enAdminSettings from '../locales/en/admin/settings'
import zhAdminAccounts from '../locales/zh/admin/accounts'
import zhAdminChannels from '../locales/zh/admin/channels'
import zhAdminOps from '../locales/zh/admin/ops'
import zhAdminOverview from '../locales/zh/admin/overview'
import zhAdminResources from '../locales/zh/admin/resources'
import zhAdminSettings from '../locales/zh/admin/settings'

// locales/{zh,en}/index.ts 与 admin/index.ts 使用对象展开聚合各域模块，
// 展开模块之间若出现同名顶层键会静默覆盖。本测试将该风险固化为显式失败。
type Modules = Record<string, Record<string, unknown>>

function collisions(modules: Modules): string[] {
  const seen = new Map<string, string>()
  const out: string[] = []
  for (const [name, mod] of Object.entries(modules)) {
    for (const key of Object.keys(mod)) {
      const prev = seen.get(key)
      if (prev) {
        out.push(`"${key}" in both ${prev} and ${name}`)
      } else {
        seen.set(key, name)
      }
    }
  }
  return out
}

/**
 * 根语言包模块自动收集。
 *
 * 以前这里是手写白名单，新增模块（如 tickets）不会被纳入检查，
 * 结果「模块导出同名顶层键把别的命名空间整块覆盖」这类问题测不出来。
 * 改用 import.meta.glob 全量扫描，新增模块自动生效。
 */
type LocaleModule = { default: Record<string, unknown> }

/**
 * 只收集 index.ts 里真正用 `...name` 展开的模块。
 *
 * 不能把目录下所有文件都算进来：像 `adminAccessKeys` 是整体赋值（`adminAccessKeys,`），
 * 它的内部键位于子命名空间，不会与顶层冲突——算进来就是误报。
 * 所以这里解析 index.ts，拿到「import 标识符 → 文件名」和「被展开的标识符」，
 * 只对这些模块做顶层键冲突检查。新增模块只要是展开写法就会自动纳入。
 */
function collectSpreadModules(locale: 'zh' | 'en', glob: Record<string, unknown>): Modules {
  const indexPath = path.resolve(process.cwd(), 'src/i18n/locales', locale, 'index.ts')
  const source = fs.readFileSync(indexPath, 'utf8')

  const imports = new Map<string, string>()
  for (const match of source.matchAll(/import\s+(\w+)\s+from\s+'\.\/([\w.-]+)'/g)) {
    imports.set(match[1], match[2])
  }
  const spreadIds = [...source.matchAll(/^\s*\.\.\.(\w+),?\s*$/gm)].map((m) => m[1])

  const out: Modules = {}
  for (const id of spreadIds) {
    const file = imports.get(id)
    if (!file) continue
    const entry = Object.entries(glob).find(([path]) => path.endsWith(`/${file}.ts`))
    if (!entry) continue
    out[file] = (entry[1] as LocaleModule).default
  }
  return out
}

const roots: Record<string, Modules> = {
  zh: collectSpreadModules('zh', import.meta.glob('../locales/zh/*.ts', { eager: true })),
  en: collectSpreadModules('en', import.meta.glob('../locales/en/*.ts', { eager: true }))
}

const admins: Record<string, Modules> = {
  zh: {
    overview: zhAdminOverview,
    channels: zhAdminChannels,
    accounts: zhAdminAccounts,
    resources: zhAdminResources,
    ops: zhAdminOps,
    settings: zhAdminSettings
  },
  en: {
    overview: enAdminOverview,
    channels: enAdminChannels,
    accounts: enAdminAccounts,
    resources: enAdminResources,
    ops: enAdminOps,
    settings: enAdminSettings
  }
}

describe.each(Object.keys(roots))('locale %s spread assembly', (locale) => {
  it('扫到了全部根模块（防止 glob 失效导致空跑）', () => {
    expect(Object.keys(roots[locale]).length).toBeGreaterThan(7)
  })

  it('root modules have no overlapping top-level keys', () => {
    expect(collisions(roots[locale])).toEqual([])
  })

  it('root modules do not shadow the explicit "admin" namespace', () => {
    for (const [name, mod] of Object.entries(roots[locale])) {
      expect(Object.keys(mod), `module ${name} must not define "admin"`).not.toContain('admin')
    }
  })

  it('admin modules have no overlapping top-level keys', () => {
    expect(collisions(admins[locale])).toEqual([])
  })
})
