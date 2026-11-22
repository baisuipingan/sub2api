<template>
  <section id="workbench-search" class="card p-5 sm:p-6" :aria-busy="busy">
    <div class="mb-5 flex flex-wrap items-start justify-between gap-3">
      <div>
        <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.searchProviders.title') }}</h2>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.searchProviders.hint') }}</p>
      </div>
      <button type="button" class="btn btn-ghost btn-sm" :disabled="busy" @click="load">{{ t('common.refresh') }}</button>
    </div>
    <fieldset class="min-w-0 space-y-4" :disabled="busy || !saved">
      <div class="grid max-w-3xl items-start gap-4 sm:grid-cols-2">
        <div>
          <label for="workbench-search-provider" class="input-label mb-1.5 block">{{ t('admin.workbenchSettings.searchProviders.provider') }}</label>
          <Select id="workbench-search-provider" :model-value="provider" :options="providers" :aria-label="t('admin.workbenchSettings.searchProviders.provider')" :disabled="busy || !saved" @update:model-value="selectProvider" />
        </div>
        <Input v-if="provider" id="workbench-search-key" v-model="apiKey" type="password" autocomplete="new-password" :maxlength="1024" :label="t('admin.workbenchSettings.searchProviders.key')" :placeholder="keyCanBeKept ? t('admin.workbenchSettings.searchProviders.keepKey') : undefined" />
        <Input
          v-if="provider === 'deepseek'"
          id="workbench-search-price"
          v-model="priceInput"
          type="number"
          min="0"
          :max="usdToDisplay(1_000_000)"
          step="any"
          :label="t('admin.workbenchSettings.searchProviders.price', { currency: view.displayCurrency })"
          :hint="t('admin.workbenchSettings.searchProviders.priceHint')"
          :error="priceInput && price === undefined ? t('admin.workbenchSettings.searchProviders.invalidPrice', { min: displayPrice(0.00001), max: displayPrice(1_000_000) }) : undefined"
        />
      </div>
      <button type="button" class="btn btn-primary btn-sm" :disabled="!canSave" @click="save">{{ t('common.save') }}</button>
      <div v-if="saved?.api_key_configured && !dirty" class="border-t border-gray-100 pt-4 dark:border-dark-700">
        <div class="flex max-w-3xl flex-wrap items-end gap-3">
          <div class="min-w-44 flex-1">
            <Input id="workbench-search-query" v-model="query" :maxlength="128" :label="t('admin.workbenchSettings.searchProviders.testQuery')" :placeholder="t('admin.workbenchSettings.searchProviders.sampleQuery')" @enter="runTest" />
          </div>
          <button type="button" class="btn btn-secondary btn-sm" :disabled="busy" @click="runTest">{{ t('admin.workbenchSettings.searchProviders.test') }}</button>
        </div>
        <p v-if="result" role="status" class="mt-3 text-sm text-gray-600 dark:text-dark-300">{{ t('admin.workbenchSettings.searchProviders.result', { count: result.results.length }) }}</p>
        <ul v-if="result?.results.length" class="mt-2 space-y-3 text-sm">
          <li v-for="(source, index) in result.results" :key="index" class="break-words">
            <a v-if="safeLink(source.url)" :href="safeLink(source.url)" target="_blank" rel="noopener noreferrer" class="font-medium text-primary-600 hover:underline dark:text-primary-400">{{ source.title || source.url }}</a>
            <span v-else>{{ source.title }}</span>
            <p v-if="source.snippet" class="mt-0.5 text-gray-500 dark:text-dark-400">{{ source.snippet }}</p>
          </li>
        </ul>
      </div>
    </fieldset>
    <p v-if="error" role="alert" class="mt-3 break-words text-sm text-red-600 dark:text-red-400">{{ error }}</p>
    <TotpStepUpDialog :controller="stepUp" />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { getWorkbenchSearchConfig, testWorkbenchSearch, updateWorkbenchSearchConfig, type WorkbenchSearchConfig, type WorkbenchSearchProvider, type WorkbenchSearchTestResult } from '@/api/admin/workbench'
import { isStepUpBlocked, isStepUpCancelled, stepUpBlockReason, useStepUp } from '@/composables/useStepUp'
import { useBillingCurrency } from '@/composables/useBillingCurrency'
import { displayToUSD as amountToUSD } from '@/utils/billingCurrency'
import { sanitizeUrl } from '@/utils/url'
import { useAppStore } from '@/stores'
import Input from '@/components/common/Input.vue'
import Select from '@/components/common/Select.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'

const emit = defineEmits<{ configured: [configured: boolean] }>()
const { t } = useI18n()
const { view, usdToDisplay, displayToUSD } = useBillingCurrency()
const app = useAppStore()
const stepUp = useStepUp()
const providers = computed(() => [
  { value: '', label: t('admin.workbenchSettings.searchProviders.none') },
  { value: 'deepseek', label: 'DeepSeek' },
  { value: 'brave', label: 'Brave' },
  { value: 'tavily', label: 'Tavily' },
])
const saved = ref<WorkbenchSearchConfig>()
const provider = ref<WorkbenchSearchProvider>('')
const apiKey = ref('')
const priceInput = ref('0')
const query = ref('')
const result = ref<WorkbenchSearchTestResult>()
const error = ref('')
const busy = ref(false)
let disposed = false

const keyCanBeKept = computed(() => saved.value?.api_key_configured && provider.value === saved.value.provider)
function displayPrice(usd: number) {
  return Number(usdToDisplay(usd).toPrecision(15))
}
const price = computed(() => {
  if (!priceInput.value.trim()) return undefined
  const input = Number(priceInput.value)
  const savedPrice = saved.value?.price_per_1000_usd ?? 0
  const amount = provider.value === saved.value?.provider && input === displayPrice(savedPrice)
    ? savedPrice
    : Number(displayToUSD(input).toPrecision(15))
  // One search must cost at least one credit (1 USD = 100,000,000 credits).
  return amount === 0 || (amount >= 0.00001 && amount <= 1_000_000) ? amount : undefined
})
watch(view, (next, previous) => {
  if (next.displayCurrency === previous.displayCurrency && next.usdToCny === previous.usdToCny) return
  if (!priceInput.value.trim() || !Number.isFinite(Number(priceInput.value))) return
  priceInput.value = String(displayPrice(amountToUSD(Number(priceInput.value), previous)))
})
const priceChanged = computed(() => provider.value === 'deepseek' && price.value !== (saved.value?.price_per_1000_usd ?? 0))
const dirty = computed(() => provider.value !== saved.value?.provider || !!apiKey.value.trim() || priceChanged.value)
const canSave = computed(() =>
  !!saved.value && !busy.value && dirty.value &&
  (provider.value !== 'deepseek' || price.value !== undefined) &&
  (!provider.value || keyCanBeKept.value || !!apiKey.value.trim()),
)

function selectProvider(value: unknown) {
  if (value === '' || value === 'deepseek' || value === 'brave' || value === 'tavily') {
    provider.value = value
    apiKey.value = ''
    priceInput.value = String(displayPrice(value === saved.value?.provider ? saved.value.price_per_1000_usd ?? 0 : 0))
    result.value = undefined
    error.value = ''
  }
}

function apply(config: WorkbenchSearchConfig) {
  saved.value = config
  provider.value = config.provider
  apiKey.value = ''
  priceInput.value = String(displayPrice(config.price_per_1000_usd ?? 0))
  result.value = undefined
}

function reportError(cause: unknown) {
  if (disposed || isStepUpCancelled(cause)) return
  if (isStepUpBlocked(cause)) {
    error.value = t(stepUpBlockReason(cause) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
    return
  }
  error.value = (cause as { message?: string })?.message || t('errors.networkError')
}

async function load() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const config = await getWorkbenchSearchConfig()
    if (!disposed) apply(config)
  } catch (cause) {
    if (!disposed) {
      saved.value = undefined
      reportError(cause)
    }
  } finally { busy.value = false }
}

async function save() {
  if (!canSave.value) return
  busy.value = true
  error.value = ''
  const key = apiKey.value.trim()
  const payload = {
    provider: provider.value,
    ...(provider.value && key ? { api_key: key } : {}),
    ...(provider.value === 'deepseek' ? { price_per_1000_usd: price.value } : {}),
  }
  try {
    const config = await stepUp.run(() => updateWorkbenchSearchConfig(payload))
    if (!disposed) {
      apply(config)
      emit('configured', config.api_key_configured)
      app.showSuccess(t('admin.workbenchSettings.searchProviders.saved'))
    }
  } catch (cause) { reportError(cause) }
  finally { busy.value = false }
}

async function runTest() {
  if (busy.value || dirty.value || !saved.value?.api_key_configured) return
  busy.value = true
  error.value = ''
  result.value = undefined
  try {
    const text = query.value.trim() || t('admin.workbenchSettings.searchProviders.sampleQuery')
    const response = await stepUp.run(() => testWorkbenchSearch(text))
    if (!disposed) result.value = response
  } catch (cause) { reportError(cause) }
  finally { busy.value = false }
}

function safeLink(raw: string) {
  const href = sanitizeUrl(raw)
  if (!href) return ''
  const url = new URL(href)
  return url.username || url.password ? '' : href
}

onMounted(load)
onBeforeUnmount(() => { disposed = true })
</script>
