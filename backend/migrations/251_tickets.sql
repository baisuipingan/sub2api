-- 用户工单：用户提问 → 管理员回复 → 用户追问，直到关闭。
--
-- status 语义（与前端展示一一对应）：
--   pending  待管理员处理 —— 最后一条消息来自用户
--   replied  待用户回复   —— 最后一条消息来自管理员
--   closed   已关闭       —— 管理员关闭后用户不可再回复
--
-- last_message_at / last_message_role 是为了列表排序和状态判断做的冗余字段，
-- 避免每次列列表都要额外聚合 ticket_messages。

CREATE TABLE IF NOT EXISTS tickets (
    id                BIGSERIAL PRIMARY KEY,
    user_id           BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title             TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'pending',
    priority          TEXT NOT NULL DEFAULT 'normal',
    last_message_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_message_role TEXT NOT NULL DEFAULT 'user',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_tickets_user_id ON tickets (user_id, last_message_at DESC);
CREATE INDEX IF NOT EXISTS idx_tickets_status ON tickets (status, last_message_at DESC);

CREATE TABLE IF NOT EXISTS ticket_messages (
    id          BIGSERIAL PRIMARY KEY,
    ticket_id   BIGINT NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,
    sender_id   BIGINT REFERENCES users(id) ON DELETE SET NULL,
    sender_role TEXT NOT NULL,
    content     TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ticket_messages_ticket ON ticket_messages (ticket_id, created_at);

COMMENT ON COLUMN tickets.status IS 'pending=待管理员处理, replied=待用户回复, closed=已关闭';
COMMENT ON COLUMN tickets.priority IS 'normal=普通, high=紧急';
COMMENT ON COLUMN ticket_messages.sender_role IS 'user=用户, admin=管理员';
