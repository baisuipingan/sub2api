import { mount } from '@vue/test-utils'
import { describe, expect, it, vi } from 'vitest'
import EventEditorDialog from '../EventEditorDialog.vue'
import type { AdminGroup, TeamEvent } from '@/types'

vi.mock('vue-i18n', () => ({ useI18n: () => ({ t: (key: string) => key }) }))

const event: TeamEvent = {
  id: 12,
  category_id: null,
  title: '已取消的定向活动',
  summary: '摘要',
  description_markdown: '详情',
  tags: ['AI'],
  organizer_name: 'Sub2API',
  organizer_url: 'https://example.com',
  fee_type: 'free',
  currency: 'CNY',
  registration_url: 'https://example.com/register',
  cover_url: 'https://example.com/cover.png',
  status: 'cancelled',
  phase: 'cancelled',
  visibility: 'targeted',
  audience: { subscription_group_ids: [7] },
  visible_from: '2026-07-01T00:00:00Z',
  visible_until: '2026-08-01T00:00:00Z',
  cancelled_reason: '场地原因',
  occurrences: [{
    id: 1,
    starts_at: '2026-07-20T02:00:00Z',
    ends_at: '2026-07-20T04:00:00Z',
    timezone: 'Asia/Shanghai',
    all_day: false,
    location_mode: 'offline',
    venue_name: '活动场地',
    address: '详细地址',
    country: '中国',
    province: '上海市',
    city: '上海',
    district: '浦东新区',
    latitude: 31.2304,
    longitude: 121.4737,
    coordinate_source: 'wgs84',
  }],
  created_at: '2026-06-01T00:00:00Z',
  updated_at: '2026-06-02T00:00:00Z',
}

const group = {
  id: 7,
  name: '团队订阅分组',
  platform: 'openai',
  status: 'active',
} as AdminGroup

describe('EventEditorDialog', () => {
  it('preserves cancelled status and targeted visibility on save', async () => {
    const wrapper = mount(EventEditorDialog, {
      props: {
        show: true,
        event,
        categories: [],
        groups: [group],
        mapSettings: null,
        saving: false,
      },
      global: {
        stubs: {
          BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
          EventMap: true,
          Icon: true,
        },
      },
    })

    await wrapper.get('form').trigger('submit')

    const payload = wrapper.emitted('save')?.[0]?.[0]
    expect(payload).toMatchObject({
      status: 'cancelled',
      cancelled_reason: '场地原因',
      visibility: 'targeted',
      audience: { subscription_group_ids: [7] },
      cover_url: 'https://example.com/cover.png',
    })
    expect(payload.visible_from).toBe('2026-07-01T00:00:00.000Z')
    expect(payload.visible_until).toBe('2026-08-01T00:00:00.000Z')
  })
})
