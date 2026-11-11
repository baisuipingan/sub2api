-- Agent 用途独立于可变显示名称；不领用或改写历史生图工作台密钥。
ALTER TABLE api_keys ADD COLUMN IF NOT EXISTS purpose VARCHAR(32);
CREATE UNIQUE INDEX IF NOT EXISTS uq_api_keys_system_purpose_per_user
  ON api_keys (user_id, purpose)
  WHERE key_type = 'system' AND deleted_at IS NULL AND purpose IS NOT NULL;
COMMENT ON COLUMN api_keys.purpose IS
  '系统托管用途身份；历史 Key 为 NULL，desktop_agent 与生图 Key 独立';
