-- 工单软删除。
--
-- 不物理删除：工单是纠纷处理依据，误删后无法追查。
-- 用户可删自己的工单（清理误建），管理员可删任意工单（清理垃圾/广告）。
-- 删除后列表与服务端读取一律过滤，但数据仍留在库里。

ALTER TABLE tickets ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_tickets_active
    ON tickets (last_message_at DESC)
    WHERE deleted_at IS NULL;

COMMENT ON COLUMN tickets.deleted_at IS '软删除时间，非空表示已删除';
