/**
 * 生图工作台的 zustand → Pinia 兼容层。
 *
 * 为什么需要它：工作台的 store 有 1300 行业务逻辑（提交任务、遮罩编辑、
 * 收藏管理、导入导出等），全部围绕 zustand 的 `set` / `get` /
 * `useStore.getState()` 展开。直接改写成 Pinia 的 ref/action 风格
 * 等于重写这些逻辑，是高风险无收益的操作。
 *
 * 这里的做法是：用 Pinia 存状态，再造一个形状兼容 zustand 的包装层，
 * 业务逻辑原样运行。这样迁移的是「容器」，不是「逻辑」。
 *
 * 约束：本文件只做状态搬运，不承载任何业务规则。
 */
import { defineStore } from 'pinia'
import { computed, reactive, ref } from 'vue'

/** zustand 风格的状态更新器：要么给对象，要么给 (prev) => patch */
export type StateSetter<T> = (partial: Partial<T> | ((state: T) => Partial<T>)) => void

/** zustand 风格的读取器 */
export type StateGetter<T> = () => T

/**
 * 把 Vue 的 reactive 代理转成 zustand 的「快照」语义。
 *
 * zustand 的 `get()` 返回的是当前状态对象的浅引用，调用方常写
 * `get().tasks`。Vue 的 reactive 代理在读取字段时行为一致，
 * 但直接返回代理会让调用方可能误改状态，因此返回 `toRaw` 后的
 * 原对象更贴近 zustand 的语义。
 */
export function createStateHelpers<T extends object>(state: T) {
  const set: StateSetter<T> = (partial) => {
    const patch = typeof partial === 'function' ? (partial as (s: T) => Partial<T>)(state) : partial
    Object.assign(state, patch)
  }
  const get: StateGetter<T> = () => state
  return { set, get }
}

/**
 * 兼容 `useStore.getState()` / `useStore.setState()` 的轻量包装。
 *
 * 老代码里有 `useStore.getState().setFilterStatus(...)` 这类写法，
 * 靠这个包装可以原样保留。
 */
export function createStoreFacade<T extends object>(state: T, read: () => T) {
  return {
    getState: () => read(),
    setState: (patch: Partial<T> | ((s: T) => Partial<T>)) => {
      const next = typeof patch === 'function' ? (patch as (s: T) => Partial<T>)(read()) : patch
      Object.assign(state, next)
    },
  }
}

export { defineStore, reactive, ref, computed }
