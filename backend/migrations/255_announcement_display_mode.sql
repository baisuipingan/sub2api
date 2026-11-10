-- Existing announcements retain their current notification and read state.
ALTER TABLE announcements ADD COLUMN IF NOT EXISTS display_mode VARCHAR(20) NOT NULL DEFAULT 'timeline';
ALTER TABLE announcements ADD CONSTRAINT announcements_display_mode_check CHECK (display_mode IN ('notice','timeline'));
CREATE INDEX IF NOT EXISTS idx_announcements_display_created ON announcements(display_mode,created_at DESC,id DESC) WHERE status='active';
