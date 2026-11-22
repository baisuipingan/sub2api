<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="card p-5 sm:p-6" data-testid="workbench-access-settings" :aria-busy="loading">
        <div class="mb-5 flex flex-wrap items-start justify-between gap-3">
          <div>
            <h2 class="text-base font-semibold text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.accessTitle') }}</h2>
            <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.accessHint') }}</p>
          </div>
          <button type="button" class="btn btn-ghost btn-sm" :disabled="loading || saving" @click="loadConfig">{{ t('common.refresh') }}</button>
        </div>
        <p v-if="loadError" class="mb-4 break-words text-sm text-red-600 dark:text-red-400" role="alert">{{ loadError }}</p>
        <fieldset class="min-w-0 space-y-5" :disabled="loading || saving || !loaded">
          <div class="flex items-start justify-between gap-4 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <div>
              <label for="workbench-entry-enabled" class="text-sm font-medium text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.entryEnabled') }}</label>
              <p id="workbench-entry-description" class="mt-1 text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.entryHint') }}</p>
            </div>
            <Toggle id="workbench-entry-enabled" v-model="form.image_studio_enabled" :aria-label="t('admin.workbenchSettings.entryEnabled')" aria-describedby="workbench-entry-description" />
          </div>
          <div class="grid gap-3 sm:grid-cols-2">
 <div v-for="feature in features" :key="feature.key" class="flex items-start justify-between gap-3 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
 <div>
   <label :for="feature.key" class="text-sm font-medium">{{ feature.label }}</label>
   <p class="mt-1 text-xs leading-5 text-gray-500">{{ feature.hint }}</p>
   <div v-if="feature.key === 'search_enabled'" class="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs">
     <span v-if="searchProviderStatus" :class="searchProviderStatus === 'configured' ? 'text-green-700 dark:text-green-400' : 'text-amber-700 dark:text-amber-300'" role="status">{{ t(`admin.workbenchSettings.searchProviders.${searchProviderStatus}`) }}</span>
     <router-link to="/admin/workbench/settings#workbench-search" class="font-medium text-primary-600 hover:underline dark:text-primary-400">{{ t('admin.workbenchSettings.searchProviders.configure') }}</router-link>
   </div>
 </div>
 <Toggle :id="feature.key" :model-value="form[feature.key]" :aria-label="feature.label" @update:model-value="setFeature(feature.key, $event)" />
 </div></div>
          <div class="grid gap-4 sm:grid-cols-2">
            <div class="min-w-0">
              <label for="workbench-group" class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('admin.workbenchSettings.group') }}</label>
              <Select id="workbench-group" :model-value="form.image_studio_group_id" :options="groupOptions" :searchable="groupOptions.length > 6" :aria-label="t('admin.workbenchSettings.group')" :placeholder="t('admin.workbenchSettings.groupPlaceholder')" @update:model-value="form.image_studio_group_id = Number($event) || 0" />
              <p v-if="loaded && form.image_studio_enabled && !selectedGroupAvailable" class="mt-2 text-xs leading-5 text-amber-700 dark:text-amber-300" role="note">{{ t('admin.workbenchSettings.groupRequired') }}</p>
            </div>
            <div class="min-w-0">
              <h3 class="mb-2 text-sm font-medium text-gray-700 dark:text-dark-200">{{ t('admin.workbenchSettings.agentGroup') }}</h3>
              <div class="flex flex-wrap items-baseline gap-x-3 gap-y-1 text-sm">
                <span id="workbench-agent-group" class="min-w-0 break-words">{{ agentGroupLabel }}</span>
                <router-link to="/admin/settings#agent" class="font-medium text-primary-600 hover:underline dark:text-primary-400">{{ t('admin.workbenchSettings.manageAgentGroup') }}</router-link>
              </div>
              <p v-if="loaded && form.chat_enabled && !agentGroupAvailable" class="mt-2 text-xs leading-5 text-amber-700 dark:text-amber-300" role="note">{{ t('admin.workbenchSettings.agentGroupRequired') }}</p>
            </div>
          </div>
          <p class="text-xs leading-5 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.rateLimitHint') }}</p>
          <details class="rounded-xl border border-gray-200 p-4 dark:border-dark-700">
            <summary class="cursor-pointer text-sm font-medium">自定义模型能力</summary>
            <p class="mt-2 text-xs leading-5 text-gray-500 dark:text-dark-400">官方模型名会自动识别。仅在分组使用自定义公开别名时，将它关联到对应的官方能力档案；不会修改模型路由、价格或官方参数上限。</p>
            <p v-if="profileLoadError" role="alert" class="mt-3 text-sm text-amber-700 dark:text-amber-300">{{ profileLoadError }} <button type="button" class="underline" :disabled="profilesLoading" @click="loadProfiles">重新加载</button></p>
            <div v-for="(row, index) in profileRows" :key="row.key" class="mt-4 grid items-end gap-3 sm:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_auto]" data-testid="workbench-model-profile">
              <Input :id="`workbench-model-alias-${row.key}`" v-model="row.modelId" :label="`公开模型 ID ${index + 1}`" placeholder="分组向用户展示的模型 ID" />
              <div>
                <label :for="`workbench-model-profile-${row.key}`" class="mb-2 block text-sm font-medium text-gray-700 dark:text-dark-200">官方能力档案</label>
                <Select :id="`workbench-model-profile-${row.key}`" v-model="row.profileId" :options="profileOptions(row.profileId)" searchable :aria-label="`官方能力档案 ${index + 1}`" placeholder="选择官方模型" :disabled="!profilesReady" />
              </div>
              <button type="button" class="btn btn-ghost btn-sm" :aria-label="`删除模型能力关联 ${index + 1}`" @click="profileRows.splice(index, 1)">删除</button>
            </div>
            <button type="button" class="btn btn-secondary btn-sm mt-4" :disabled="!profilesReady || profileRows.length >= 128" @click="addProfile">添加模型别名</button>
            <p v-if="profileError" role="alert" class="mt-3 text-sm text-red-600">{{ profileError }}</p>
          </details>
          <div class="flex flex-wrap items-center gap-3">
            <button type="button" data-testid="workbench-access-save" class="btn btn-primary btn-sm" :disabled="!canSave" @click="saveConfig">{{ saving ? t('common.loading') : t('common.save') }}</button>
            <router-link to="/admin/groups" class="text-sm text-primary-600 hover:text-primary-700 dark:text-primary-400">{{ t('admin.workbenchSettings.manageGroups') }}</router-link>
          </div>
        </fieldset>
      </section>

      <WorkbenchSearchSettings @configured="searchProviderStatus = $event ? 'configured' : 'unconfigured'" />

      <ImageStorageSettings />
      <OfficeStorageSettings />

      <section class="rounded-xl border border-gray-200 p-5 dark:border-dark-700">
        <h2 class="text-sm font-semibold text-gray-900 dark:text-white">{{ t('admin.workbenchSettings.routingTitle') }}</h2>
        <p class="mt-1 text-sm leading-6 text-gray-500 dark:text-dark-400">{{ t('admin.workbenchSettings.routingHint') }}</p>
        <div class="mt-3 flex flex-wrap gap-3">
          <router-link to="/admin/accounts" class="btn btn-secondary btn-sm">{{ t('admin.workbenchSettings.manageAccounts') }}</router-link>
          <router-link to="/admin/channels" class="btn btn-secondary btn-sm">{{ t('admin.workbenchSettings.manageChannels') }}</router-link>
        </div>
      </section>
      <TotpStepUpDialog :controller="stepUp" />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { adminAPI } from '@/api'
import { getWorkbenchConfig, updateWorkbenchConfig, type WorkbenchConfig } from '@/api/admin/workbench'
import { getDesktopAgentProfiles, type DesktopAgentProfile } from '@/api/admin/desktopAgent'
import { isDesktopAgentGroupEligible, resolveDesktopAgentProfile } from '@/utils/desktopAgentConfig'
import type { AdminGroup } from '@/types'
import { useAppStore } from '@/stores'
import AppLayout from '@/components/layout/AppLayout.vue'
import Select from '@/components/common/Select.vue'
import Toggle from '@/components/common/Toggle.vue'
import Input from '@/components/common/Input.vue'
import ImageStorageSettings from '@/components/admin/workbench/ImageStorageSettings.vue'
import OfficeStorageSettings from '@/components/admin/workbench/OfficeStorageSettings.vue'
import WorkbenchSearchSettings from '@/components/admin/workbench/WorkbenchSearchSettings.vue'
import TotpStepUpDialog from '@/components/auth/TotpStepUpDialog.vue'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/composables/useStepUp'

const { t } = useI18n()
const appStore = useAppStore()
const stepUp = useStepUp()
const profiles = ref<DesktopAgentProfile[]>([])
const profilesReady = ref(false), profilesLoading = ref(false)
const profileLoadError = ref(''), profileError = ref('')
const profileRows = ref<{ key: number; modelId: string; profileId: string }[]>([])
let profileRowKey = 0
function loadBindings(config: WorkbenchConfig) {
  profileRows.value = Object.entries(config.model_profiles || {}).map(([modelId, binding]) => ({ key: ++profileRowKey, modelId, profileId: binding.profile_id }))
  profileError.value = ''
}
async function loadProfiles() {
  profilesLoading.value = true
  profileLoadError.value = ''
  try {
    const result = await getDesktopAgentProfiles()
    if (disposed) return
    profiles.value = result.profiles
    profilesReady.value = true
  } catch {
    if (!disposed) profileLoadError.value = '官方能力档案暂时无法加载，已有模型关联会保留。'
  } finally { if (!disposed) profilesLoading.value = false }
}
function profileOptions(selected: string) {
  return [
    { value: '', label: '选择官方模型' },
    ...(selected && !profiles.value.some(profile => profile.id === selected) ? [{ value: selected, label: `${selected}（档案暂不可用）` }] : []),
    ...profiles.value.map(profile => ({ value: profile.id, label: profile.display_name })),
  ]
}
function addProfile() { profileRows.value.push({ key: ++profileRowKey, modelId: '', profileId: '' }) }
function profilePayload(): NonNullable<WorkbenchConfig['model_profiles']> {
  const bindings: NonNullable<WorkbenchConfig['model_profiles']> = Object.create(null)
  if (profileRows.value.length > 128) throw new Error('最多配置 128 个模型别名')
  for (const row of profileRows.value) {
    const modelId = row.modelId.trim()
    if (!modelId || !row.profileId) throw new Error('请填写公开模型 ID 并选择官方能力档案，或删除空行')
    if (Object.prototype.hasOwnProperty.call(bindings, modelId)) throw new Error(`公开模型 ID 重复：${modelId}`)
    const binding = { profile_id: row.profileId, override: false as const }
    if (profilesReady.value && !resolveDesktopAgentProfile(profiles.value, modelId, binding)) throw new Error(`模型 ${modelId} 的官方能力档案不匹配`)
    bindings[modelId] = binding
  }
  return bindings
}
const form = ref({ image_studio_enabled: true, image_studio_group_id: 0, chat_enabled:false,office_enabled:false,search_enabled:false,files_enabled:false })
const agentGroupID = ref(0)
const searchProviderStatus = ref<WorkbenchConfig['search_provider_status']>()
const features = [
 {key:'chat_enabled' as const,label:'对话助手',hint:'开放云端对话。关闭后停止搜索和文档工具，历史会话和文件仍可查看。'},
 {key:'search_enabled' as const,label:t('admin.workbenchSettings.searchEnabled'),hint:t('admin.workbenchSettings.searchHint')},
 {key:'files_enabled' as const,label:'我的文件',hint:'保存私有资料、成果和版本，需配置对象存储。关闭后停用文档工具。'},
 {key:'office_enabled' as const,label:'文档工具',hint:'允许对话助手读取、生成和修改 DOCX 文档。启用时同时打开对话助手和我的文件。'},
]
type FeatureKey = typeof features[number]['key']
function setFeature(key: FeatureKey, value: boolean) {
  form.value[key] = value
  if (key === 'chat_enabled' && !value) { form.value.search_enabled = false; form.value.office_enabled = false }
  if (key === 'files_enabled' && !value) form.value.office_enabled = false
  if ((key === 'search_enabled' || key === 'office_enabled') && value) form.value.chat_enabled = true
  if (key === 'office_enabled' && value) form.value.files_enabled = true
}
const groups = ref<AdminGroup[]>([])
const loaded = ref(false)
const loading = ref(false)
const saving = ref(false)
const loadError = ref('')
let disposed = false

const eligibleGroups = computed(() => groups.value.filter(group => group.status === 'active' && group.allow_image_generation))
const selectedGroupAvailable = computed(() => eligibleGroups.value.some(group => group.id === form.value.image_studio_group_id))
const agentGroup = computed(() => groups.value.find(group => group.id === agentGroupID.value))
const agentGroupAvailable = computed(() => !!agentGroup.value && isDesktopAgentGroupEligible(agentGroup.value))
const agentGroupLabel = computed(() => agentGroup.value?.name ?? (
  agentGroupID.value > 0 ? t('admin.workbenchSettings.agentGroupUnavailable', { id: agentGroupID.value }) : t('admin.workbenchSettings.agentGroupUnconfigured')
))
const canSave = computed(() => loaded.value && !loading.value && !saving.value
  && (!form.value.image_studio_enabled || selectedGroupAvailable.value)
  && (!form.value.chat_enabled || agentGroupAvailable.value))
const groupOptions = computed(() => [
  { value: 0, label: t('admin.workbenchSettings.groupPlaceholder') },
  ...(form.value.image_studio_group_id > 0 && !selectedGroupAvailable.value
    ? [{ value: form.value.image_studio_group_id, label: t('admin.workbenchSettings.groupUnavailable', { id: form.value.image_studio_group_id }), disabled: true }]
    : []),
  ...eligibleGroups.value.map(group => ({ value: group.id, label: group.name })),
])

function errorMessage(error: unknown) {
  if (isStepUpBlocked(error)) return t(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  return (error as { message?: string })?.message || t('errors.networkError')
}

async function loadConfig() {
  if (disposed) return
  loading.value = true
  loadError.value = ''
  try {
    const [config, availableGroups] = await Promise.all([getWorkbenchConfig(), adminAPI.groups.getAll()])
    if (disposed) return
    form.value = { image_studio_enabled: config.image_studio_enabled, image_studio_group_id: config.image_studio_group_id,chat_enabled:!!config.chat_enabled,office_enabled:!!config.office_enabled,search_enabled:!!config.search_enabled,files_enabled:!!config.files_enabled }
    agentGroupID.value = config.agent_group_id ?? 0
    searchProviderStatus.value = config.search_provider_status
    loadBindings(config)
    groups.value = availableGroups
    loaded.value = true
  } catch (error) {
    if (!disposed) { loaded.value = false; loadError.value = errorMessage(error) }
  } finally { if (!disposed) loading.value = false }
}

async function saveConfig() {
  if (!canSave.value) return
  let modelProfiles: NonNullable<WorkbenchConfig['model_profiles']>
  profileError.value = ''
  try { modelProfiles = profilePayload() } catch (error) { profileError.value = errorMessage(error); appStore.showError(profileError.value); return }
  saving.value = true
  const payload = { ...form.value, model_profiles: modelProfiles }
  try {
    const saved = await stepUp.run(() => updateWorkbenchConfig(payload))
    if (disposed) return
    form.value = { image_studio_enabled: saved.image_studio_enabled, image_studio_group_id: saved.image_studio_group_id,chat_enabled:!!saved.chat_enabled,office_enabled:!!saved.office_enabled,search_enabled:!!saved.search_enabled,files_enabled:!!saved.files_enabled }
    agentGroupID.value = saved.agent_group_id ?? 0
    searchProviderStatus.value = saved.search_provider_status
    loadBindings(saved)
    appStore.showSuccess(t('admin.workbenchSettings.saved'))
    await appStore.fetchPublicSettings(true)
  } catch (error) {
    if (!disposed && !isStepUpCancelled(error)) appStore.showError(errorMessage(error))
  } finally { if (!disposed) saving.value = false }
}

onMounted(() => { void loadConfig(); void loadProfiles() })
onBeforeUnmount(() => { disposed = true })
</script>
