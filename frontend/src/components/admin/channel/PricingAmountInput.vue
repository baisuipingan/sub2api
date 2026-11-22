<template>
  <input
    :value="draft ?? displayValue"
    type="number"
    step="any"
    min="0"
    @input="setAmount(($event.target as HTMLInputElement).value)"
    @blur="draft = null"
  />
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useBillingCurrency } from '@/composables/useBillingCurrency'
import { toNullableNumber } from './types'

const props = defineProps<{
  modelValue?: number | string | null
  useSiteCurrency?: boolean
}>()
const emit = defineEmits<{ 'update:modelValue': [value: number | string | null] }>()
const currency = useBillingCurrency()
const draft = ref<string | null>(null)

function toUSD(value: string) {
  const amount = toNullableNumber(value)
  if (amount === null) return null
  return Number(currency.displayToUSD(amount).toPrecision(15))
}

const displayValue = computed(() => {
  if (!props.useSiteCurrency) return props.modelValue
  const amount = toNullableNumber(props.modelValue)
  return amount === null ? null : Number(currency.usdToDisplay(amount).toPrecision(15))
})

function setAmount(value: string) {
  if (!props.useSiteCurrency) {
    emit('update:modelValue', value || null)
    return
  }
  draft.value = value
  emit('update:modelValue', toUSD(value))
}

watch(() => props.modelValue, (value) => {
  if (draft.value !== null && toUSD(draft.value) !== toNullableNumber(value)) draft.value = null
})
watch([currency.view, () => props.useSiteCurrency], () => { draft.value = null })
</script>
