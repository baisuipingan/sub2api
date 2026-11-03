-- 工单已读状态：用于「有新回复」的未读红点。
--
-- 只记录两个时间点，不落「已读消息 id」：
--   user_read_at  用户最后一次打开工单的时间
--   admin_read_at 管理员最后一次打开工单的时间
--
-- 未读判定 = 工单最后一条消息时间 > 对应角色的已读时间。
-- 比逐条记录已读更省行数，且用户/管理员共用一张表即可。
-- 注意：last_message_at 是「任何消息」的时间，因此查看方自己发言不会把自己标成未读
-- （自己发言时也会同步把该角色的已读时间推到当前，见仓储 AddMessage）。

ALTER TABLE tickets
    ADD COLUMN IF NOT EXISTS user_read_at  TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS admin_read_at TIMESTAMPTZ;

-- 存量数据：视为已读，避免上线瞬间所有历史工单全变未读
UPDATE tickets SET user_read_at = last_message_at WHERE user_read_at IS NULL;
UPDATE tickets SET admin_read_at = last_message_at WHERE admin_read_at IS NULL;

COMMENT ON COLUMN tickets.user_read_at IS '用户最后查看该工单的时间，NULL 视为已读';
COMMENT ON COLUMN tickets.admin_read_at IS '管理员最后查看该工单的时间，NULL 视为已读';
