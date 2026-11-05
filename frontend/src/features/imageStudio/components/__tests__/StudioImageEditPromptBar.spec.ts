import { afterEach, describe, expect, it, vi } from 'vitest'
import { enableAutoUnmount, mount } from '@vue/test-utils'
import { DEFAULT_PARAMS } from '../../types'
import { createImageModelCatalogFromModelList } from '../../lib/modelRegistry'
import Select from '@/components/common/Select.vue'
import TextArea from '@/components/common/TextArea.vue'
import StudioImageEditPromptBar from '../imageEdit/StudioImageEditPromptBar.vue'

vi.mock('vue-i18n', async () => ({
  ...await vi.importActual<typeof import('vue-i18n')>('vue-i18n'),
  useI18n: () => ({ t: (key: string) => key }),
}))
enableAutoUnmount(afterEach)

describe('image editor shared form controls', () => {
  it('emits changes from shared text and selection controls without submitting', async () => {
    const models = createImageModelCatalogFromModelList({ data: [{ id: 'gpt-image-2.6-flare' }, { id: 'gpt-image-2.6-sunburst' }] })
    const wrapper = mount(StudioImageEditPromptBar, { props: {
      mode: 'full', prompt: '', params: { ...DEFAULT_PARAMS }, models,
      selectedModelId: models[0].id, busy: false, canUseMask: true, canRemoveBackground: true,
    } })
    expect(wrapper.find('select').exists()).toBe(false)
    expect(wrapper.findComponent(TextArea).exists()).toBe(true)
    await wrapper.findComponent(TextArea).get('textarea').setValue('Edit this image')
    expect(wrapper.emitted('promptChange')?.[0]).toEqual(['Edit this image'])
    const selects = wrapper.findAllComponents(Select)
    selects[0].vm.$emit('update:modelValue', models[1].id)
    selects[1].vm.$emit('update:modelValue', 'high')
    expect(wrapper.emitted('modelChange')?.[0]).toEqual([models[1].id])
    expect(wrapper.emitted('paramsChange')?.[0]?.[0]).toMatchObject({ quality: 'high' })
    expect(wrapper.emitted('generate')).toBeUndefined()
    expect(wrapper.emitted('save')).toBeUndefined()
  })
})
