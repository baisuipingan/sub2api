<template>
  <div class="card">
    <div class="flex flex-wrap items-start justify-between gap-4 border-b border-gray-100 px-6 py-4 dark:border-dark-700">
      <div>
        <h2 class="text-lg font-semibold text-gray-900 dark:text-white">
          {{ t('admin.settings.payment.billingCurrency.title') }}
        </h2>
        <p class="mt-1 max-w-3xl text-sm text-gray-500 dark:text-gray-400">
          {{ t('admin.settings.payment.billingCurrency.description') }}
        </p>
      </div>
      <button type="button" class="btn btn-primary" :disabled="disabled || saving || loading || !dirty" @click="save">
        {{ saving ? t('common.saving') : t('common.save') }}
      </button>
    </div>

    <div v-if="loading" class="flex justify-center p-8">
      <LoadingSpinner size="md" />
    </div>
    <div v-else class="space-y-5 p-6">
      <div class="flex flex-wrap items-center justify-between gap-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <div>
          <label class="font-medium text-gray-900 dark:text-white">
            {{ t('admin.settings.payment.billingCurrency.enabled') }}
          </label>
          <p class="text-sm text-gray-500 dark:text-gray-400">
            {{ t('admin.settings.payment.billingCurrency.enabledHint') }}
          </p>
        </div>
        <Toggle v-model="form.enabled" :disabled="disabled || saving" />
      </div>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-3">
        <div>
          <label class="input-label">{{ t('admin.settings.payment.billingCurrency.engine') }}</label>
          <Select v-model="form.engine" :options="engineOptions" :disabled="disabled || saving || !form.enabled" />
          <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
            {{ t(`admin.settings.payment.billingCurrency.engineHints.${form.engine}`) }}
          </p>
        </div>
        <div>
          <label for="billing-display-currency" class="input-label">{{ t('admin.settings.payment.billingCurrency.displayCurrency') }}</label>
          <Select
            id="billing-display-currency"
            v-model="form.display_currency"
            :options="currencyOptions"
            :disabled="disabled || saving"
            :aria-label="t('admin.settings.payment.billingCurrency.displayCurrency')"
          />
        </div>
        <div>
          <label class="input-label">{{ t('admin.settings.payment.billingCurrency.usdToCny') }}</label>
          <input
            v-model.number="form.usd_to_cny"
            type="number"
            min="0.0001"
            step="0.0001"
            class="input"
            :disabled="disabled || saving"
            @keydown.enter.prevent="save"
          />
        </div>
      </div>

      <div class="grid grid-cols-1 gap-4 md:grid-cols-2">
        <div class="rounded-xl bg-gray-50 p-4 text-sm dark:bg-dark-800">
          <p class="font-medium text-gray-800 dark:text-gray-200">
            {{ t('admin.settings.payment.billingCurrency.fixedScale') }}
          </p>
          <p class="mt-1 font-mono text-gray-600 dark:text-gray-400">
            1 USD = {{ Number(form.credits_per_usd || 100000000).toLocaleString() }} credits
          </p>
        </div>
        <div class="rounded-xl bg-amber-50 p-4 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
          {{ t('admin.settings.payment.billingCurrency.rolloutHint') }}
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import type { BillingCurrencyConfig, BillingDisplayCurrency, BillingEngine } from '@/api/admin/settings'
import { useAppStore } from '@/stores'
import { extractApiErrorMessage } from '@/utils/apiError'
import LoadingSpinner from '@/components/common/LoadingSpinner.vue'
import Toggle from '@/components/common/Toggle.vue'
import Select from '@/components/common/Select.vue'

const props = defineProps<{ disabled?: boolean }>()

const { t } = useI18n()
const appStore = useAppStore()
const loading = ref(true)
const saving = ref(false)
const savedConfig = ref<BillingCurrencyConfig | null>(null)
const form = reactive<BillingCurrencyConfig>({
  enabled: false,
  engine: 'legacy',
  credits_per_usd: 100_000_000,
  display_currency: 'USD',
  usd_to_cny: 7.2,
  recharge_rates: { USD: 1, CNY: 7.2 },
  version: 1,
})
const dirty = computed(() => savedConfig.value !== null && (
  form.enabled !== savedConfig.value.enabled ||
  form.engine !== savedConfig.value.engine ||
  form.display_currency !== savedConfig.value.display_currency ||
  form.usd_to_cny !== savedConfig.value.usd_to_cny
))

const engineOptions = [
  { value: 'legacy' as BillingEngine, label: t('admin.settings.payment.billingCurrency.engines.legacy') },
  { value: 'shadow' as BillingEngine, label: t('admin.settings.payment.billingCurrency.engines.shadow') },
  { value: 'credits' as BillingEngine, label: t('admin.settings.payment.billingCurrency.engines.credits') },
]
const currencyOptions = [
  { value: 'USD' as BillingDisplayCurrency, label: 'USD ($)' },
  { value: 'CNY' as BillingDisplayCurrency, label: 'CNY (¥)' },
]

function applyConfig(config: BillingCurrencyConfig) {
  form.enabled = config.enabled
  form.engine = config.engine
  form.credits_per_usd = config.credits_per_usd || 100_000_000
  form.display_currency = config.display_currency
  form.usd_to_cny = config.usd_to_cny || 7.2
  form.recharge_rates = {
    USD: 1,
    CNY: form.usd_to_cny,
    ...config.recharge_rates,
  }
  form.version = config.version || 1
  savedConfig.value = { ...form }
}

async function load() {
  loading.value = true
  try {
    applyConfig(await adminAPI.settings.getBillingCurrencyConfig())
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
  } finally {
    loading.value = false
  }
}

async function saveChanges(): Promise<boolean> {
  if (saving.value) return false
  const saved = savedConfig.value
  if (!saved || !dirty.value) return true
  saving.value = true
  try {
    const rechargeRates = { ...form.recharge_rates }
    // The exchange-rate field also controls the CNY recharge rate when edited.
    if (form.usd_to_cny !== saved.usd_to_cny) rechargeRates.CNY = Number(form.usd_to_cny)
    const updated = await adminAPI.settings.updateBillingCurrencyConfig({
      ...form,
      recharge_rates: rechargeRates,
    })
    applyConfig(updated)
    return true
  } catch (error) {
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
    return false
  } finally {
    saving.value = false
  }
}

async function save() {
  if (props.disabled || !dirty.value || saving.value) return
  if (await saveChanges()) {
    await appStore.fetchPublicSettings(true)
    appStore.showSuccess(t('common.saved'))
  }
}

defineExpose({ saveChanges, saving })

onMounted(load)
</script>
