import i18n from '@/i18n'

/**
 * lib 层抛错时的翻译助手。
 *
 * lib 里没有组件上下文，用全局 i18n 实例取当前语言。
 * 这些错误最终会以 toast 形式弹给用户，所以不能把中文写死在 throw 里。
 *
 * 为什么在抛出时就翻译，而不是抛 key 让上层翻译：
 * 上层有多处直接读 `error.message`（遮罩编辑器、上传流程等），
 * 逐个改容易漏；在唯一的抛出点翻译，能保证任何消费方拿到的都是人话。
 */
export function libT(key: string, params?: Record<string, unknown>): string {
  return i18n.global.t(key, params as never)
}
