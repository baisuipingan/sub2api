<template>
  <section class="bs-chat-history" aria-label="对话历史">
    <div class="bs-chat-history-heading"><span>对话</span><button type="button" class="bs-chat-icon" title="新建对话" aria-label="新建对话" @click="newChat"><Icon name="plus" size="sm" /></button></div>
    <div class="bs-chat-history-scroll" @scroll.passive="onScroll">
      <p v-if="listError" class="bs-chat-small-error" role="alert">{{ listError }}<button type="button" @click="reload">重试</button></p>
      <div v-for="conversation in store.conversations" :key="conversation.id" class="bs-chat-history-row" :class="{ active: store.activeConversationId === conversation.id, editing: renaming === conversation.id }">
        <template v-if="renaming === conversation.id">
          <input ref="renameInput" v-model="title" class="bs-chat-rename" aria-label="对话名称" maxlength="100" @keydown.enter.prevent="saveRename(conversation.id)" @keydown.esc.prevent="renaming = ''" @blur="saveRename(conversation.id)" />
          <button type="button" class="bs-chat-icon" aria-label="保存名称" @mousedown.prevent @click="saveRename(conversation.id)"><Icon name="check" size="sm" /></button>
        </template>
        <template v-else>
          <RouterLink :to="`/workbench/chat/${conversation.id}`" class="bs-chat-history-title" :title="conversation.title">{{ conversation.title || '新对话' }}</RouterLink>
          <button type="button" class="bs-chat-icon bs-chat-convo-menu" :aria-label="`${conversation.title}的更多操作`" :aria-expanded="menu === conversation.id" @click.stop="menu = menu === conversation.id ? '' : conversation.id"><Icon name="more" size="sm" /></button>
          <div v-if="menu === conversation.id" class="bs-chat-history-menu" @click.stop>
            <button type="button" @click="startRename(conversation)"><Icon name="edit" size="sm" />重命名</button>
            <button type="button" class="danger" @click="toDelete = conversation; menu = ''"><Icon name="trash" size="sm" />删除</button>
          </div>
        </template>
      </div>
      <p v-if="store.loadingConversations" class="bs-chat-muted">正在加载…</p>
      <p v-else-if="!store.conversations.length && !listError" class="bs-chat-muted">开始对话后，记录会保存在这里</p>
      <button v-if="store.nextCursor && !store.loadingConversations" type="button" class="bs-chat-load-more" @click="more">加载更多</button>
    </div>
    <ConfirmDialog :show="!!toDelete" title="删除对话" :message="`删除「${toDelete?.title || '新对话'}」及其消息？`" danger :loading="deleting" @cancel="toDelete = null" @confirm="remove" />
  </section>
</template>
<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/icons/Icon.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import { useWorkbenchChatStore, chatError } from '@/stores/workbenchChat'
import type { ChatConversation } from '@/types/workbenchChat'
import './chat.css'
const store = useWorkbenchChatStore()
const router = useRouter()
const menu = ref(''), renaming = ref(''), title = ref(''), listError = ref('')
const renameInput = ref<HTMLInputElement[] | null>(null)
const toDelete = ref<ChatConversation | null>(null), deleting = ref(false)
const closeMenu = () => { menu.value = '' }
async function reload() { listError.value = ''; try { await store.loadConversations() } catch (e) { listError.value = chatError(e) } }
async function more() { try { await store.loadMore() } catch (e) { listError.value = chatError(e) } }
function onScroll(event: Event) { const el = event.target as HTMLElement; if (el.scrollHeight - el.scrollTop - el.clientHeight < 120) void more() }
async function newChat() { await store.newConversation(); await router.push('/workbench/chat') }
async function startRename(conversation: ChatConversation) { menu.value = ''; renaming.value = conversation.id; title.value = conversation.title; await nextTick(); renameInput.value?.[0]?.select() }
async function saveRename(id: string) { if (renaming.value !== id) return; renaming.value = ''; if (!title.value.trim()) return; try { await store.renameConversation(id, title.value) } catch (e) { listError.value = chatError(e) } }
async function remove() { if (!toDelete.value) return; deleting.value = true; try { const id = toDelete.value.id; const active = store.activeConversationId === id; await store.deleteConversation(id); toDelete.value = null; if (active) await router.push('/workbench/chat') } catch (e) { listError.value = chatError(e) } finally { deleting.value = false } }
onMounted(() => { document.addEventListener('click', closeMenu); if (!store.conversations.length) void reload() })
onBeforeUnmount(() => document.removeEventListener('click', closeMenu))
</script>
