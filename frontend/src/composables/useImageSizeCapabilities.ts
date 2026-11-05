import { computed, ref, toValue } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { useI18n } from 'vue-i18n'

export const IMAGE_SIZE_CAPABILITIES_EXTRA_KEY = 'image_size_capabilities'
export const IMAGE_SIZE_TIERS = ['1k', '2k', '4k'] as const

export interface ImageSizeCapabilityRow {
  rowId: number
  model: string
  tier: string
  upstream: string
}

export function getImageSizeModelCandidates(...sources: ReadonlyArray<readonly string[]>) {
  return [...new Set(sources.flat()
    .map((model) => model.trim())
    .filter((model) => model && !model.includes('*') && !model.includes('?'))
    .filter((model) => /image|^grok-imagine|^gemini-/i.test(model)))]
}

export function useImageSizeCapabilities(modelCandidates: MaybeRefOrGetter<readonly string[]> = []) {
  const { t } = useI18n()
  let rowSequence = 0
  const rows = ref<ImageSizeCapabilityRow[]>([])
  const validationAttempted = ref(false)
  const errors = computed(() => {
    const result = new Map<number, string>()
    const seen = new Map<string, number>()
    for (const row of rows.value) {
      const model = row.model.trim()
      const tier = row.tier.trim().toLowerCase()
      if (!model || !tier || !row.upstream.trim()) {
        result.set(row.rowId, t('admin.accounts.imageSizeCapability.incompleteRule'))
        continue
      }
      if (!IMAGE_SIZE_TIERS.includes(tier as typeof IMAGE_SIZE_TIERS[number])) {
        result.set(row.rowId, t('admin.accounts.imageSizeCapability.invalidTier'))
        continue
      }
      const key = JSON.stringify([model, tier])
      const previousRowId = seen.get(key)
      if (previousRowId !== undefined) {
        const message = t('admin.accounts.imageSizeCapability.duplicateRule', { model, tier: tier.toUpperCase() })
        result.set(previousRowId, message)
        result.set(row.rowId, message)
      } else {
        seen.set(key, row.rowId)
      }
    }
    return result
  })

  function reset() {
    rows.value = []
    validationAttempted.value = false
  }

  function read(extra: Record<string, unknown> | undefined) {
    reset()
    const raw = extra?.[IMAGE_SIZE_CAPABILITIES_EXTRA_KEY]
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return
    for (const [model, tiers] of Object.entries(raw as Record<string, unknown>)) {
      const entries: [string, unknown][] = Array.isArray(tiers)
        ? tiers.flatMap((entry: unknown) => {
          if (!entry || typeof entry !== 'object' || Array.isArray(entry)) return []
          const { tier, upstream } = entry as Record<string, unknown>
          return [[typeof tier === 'string' ? tier : '', upstream]] as [string, unknown][]
        })
        : tiers && typeof tiers === 'object' ? Object.entries(tiers as Record<string, unknown>) : []
      for (const [tier, upstream] of entries) {
        rows.value.push({
          rowId: ++rowSequence,
          model: model.trim(),
          tier: tier.trim().toLowerCase(),
          upstream: typeof upstream === 'string' ? upstream.trim() : ''
        })
      }
    }
  }

  function validate() {
    validationAttempted.value = true
    return errors.value.size === 0
  }

  function build(): Record<string, Record<string, string>> | undefined {
    const table = new Map<string, Record<string, string>>()
    for (const row of rows.value) {
      const model = row.model.trim()
      const tier = row.tier.trim().toLowerCase()
      if (!table.has(model)) table.set(model, {})
      table.get(model)![tier] = row.upstream.trim()
    }
    return table.size > 0 ? Object.fromEntries(table) : undefined
  }

  function add() {
    const model = rows.value.at(-1)?.model.trim() || toValue(modelCandidates)[0] || 'gpt-image-2'
    const usedTiers = new Set(rows.value.filter((row) => row.model.trim() === model).map((row) => row.tier))
    const tier = IMAGE_SIZE_TIERS.find((candidate) => !usedTiers.has(candidate)) ?? '1k'
    rows.value.push({ rowId: ++rowSequence, model, tier, upstream: '' })
  }

  function remove(index: number) {
    rows.value.splice(index, 1)
  }

  return { rows, errors, validationAttempted, read, build, validate, reset, add, remove }
}
