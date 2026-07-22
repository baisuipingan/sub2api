export type EventStatus = 'draft' | 'published' | 'cancelled' | 'archived'
export type EventPhase = 'upcoming' | 'ongoing' | 'ended' | 'cancelled'
export type EventVisibility = 'authenticated' | 'targeted'
export type EventFeeType = 'free' | 'paid' | 'unknown'
export type EventLocationMode = 'offline' | 'online' | 'hybrid'
export type EventCoordinateSystem = 'wgs84' | 'gcj02'

export interface EventAudience {
  subscription_group_ids?: number[]
}

export interface EventCategory {
  id: number
  code: string
  name: string
  color: string
  icon: string
  sort_order: number
  enabled: boolean
}

export interface EventSource {
  id: number
  code: string
  name: string
  kind: 'manual' | 'json' | 'crawler'
  enabled: boolean
  config: Record<string, unknown>
  last_sync_at?: string
}

export interface EventMapSettings {
  enabled: boolean
  amap_key: string
  security_code: string
  default_latitude: number
  default_longitude: number
  default_zoom: number
}

export interface EventOccurrence {
  id?: number
  starts_at: string
  ends_at?: string | null
  timezone: string
  all_day: boolean
  location_mode: EventLocationMode
  online_url?: string
  venue_name?: string
  address?: string
  country?: string
  province?: string
  city?: string
  district?: string
  latitude?: number | null
  longitude?: number | null
  coordinate_source: EventCoordinateSystem
  geocode_status?: string
  geocode_precision?: string
  provider_place_id?: string
}

export interface TeamEvent {
  id: number
  category_id?: number | null
  category?: EventCategory
  title: string
  summary: string
  description_markdown: string
  tags: string[]
  organizer_name: string
  organizer_url?: string
  fee_type: EventFeeType
  price_min?: number | null
  price_max?: number | null
  currency: string
  registration_url?: string
  registration_deadline?: string | null
  cover_url?: string
  status: EventStatus
  phase: EventPhase
  visibility: EventVisibility
  audience: EventAudience
  visible_from?: string | null
  visible_until?: string | null
  published_at?: string | null
  cancelled_reason?: string
  manual_override_fields?: string[]
  occurrences: EventOccurrence[]
  created_at: string
	updated_at: string
}

export type UserEvent = Omit<TeamEvent,
  | 'visibility'
  | 'audience'
  | 'visible_from'
  | 'visible_until'
  | 'manual_override_fields'
  | 'created_at'
  | 'updated_at'
>

export interface EventWriteRequest {
  category_id?: number | null
  title: string
  summary: string
  description_markdown: string
  tags: string[]
  organizer_name: string
  organizer_url: string
  fee_type: EventFeeType
  price_min?: number | null
  price_max?: number | null
  currency: string
  registration_url: string
  registration_deadline?: string | null
  cover_url: string
  status: EventStatus
  visibility: EventVisibility
  audience: EventAudience
  visible_from?: string | null
  visible_until?: string | null
  cancelled_reason: string
  occurrences: EventOccurrence[]
}

export interface EventMapMarker {
  event_id: number
  occurrence_id: number
  title: string
  summary: string
  status: EventStatus
  phase: EventPhase
  category?: EventCategory
  fee_type: EventFeeType
  starts_at: string
  ends_at?: string | null
  venue_name?: string
  address?: string
  city?: string
  district?: string
  latitude: number
  longitude: number
}

export interface EventImportItem {
  id: number
  item_index: number
  external_id?: string
  action: 'create' | 'update' | 'unchanged' | 'conflict' | 'error'
  status: string
  event_id?: number | null
  error_code?: string
  error_detail?: string
}

export interface EventImportBatch {
  id: number
  source_id: number
  file_name: string
  schema_version: number
  mode: 'create_only' | 'upsert'
  status: 'previewed' | 'committing' | 'completed' | 'partial' | 'failed'
  total_count: number
  create_count: number
  update_count: number
  unchanged_count: number
  conflict_count: number
  error_count: number
  committed_at?: string
  created_at: string
  items: EventImportItem[]
}

export interface EventImportEnvelope {
  type: 'sub2api-events'
  version: 1
  source: string
  file_name?: string
  mode?: 'create_only' | 'upsert'
  defaults?: {
    timezone?: string
    coordinate_system?: EventCoordinateSystem
    country?: string
    province?: string
    city?: string
  }
  events: Array<Record<string, unknown>>
}
