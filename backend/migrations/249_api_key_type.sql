-- 生图工作台专用密钥标记。
--
-- 背景：生图工作台集成后不再让用户手填 API Key，改由系统按「生图工作台分组」
-- 为每个用户自动创建一把专用密钥。这类密钥需要在密钥列表中与用户自建密钥区分：
--   user    —— 用户自己创建的密钥（默认，历史数据全部属于这一类）
--   system  —— 系统托管密钥（如生图工作台），用户不可删除、不可改分组
--
-- 为什么用新增列而不是复用 name 前缀判断：前缀匹配不可靠（用户可能改名），
-- 而且列表/权限/统计都要按类型过滤，用字符串前缀做业务判断是隐患。
ALTER TABLE api_keys
  ADD COLUMN IF NOT EXISTS key_type VARCHAR(20) NOT NULL DEFAULT 'user';

-- 列表接口按 (user_id, key_type) 过滤，避免全表扫描
CREATE INDEX IF NOT EXISTS idx_api_keys_user_type
  ON api_keys (user_id, key_type)
  WHERE deleted_at IS NULL;

COMMENT ON COLUMN api_keys.key_type IS
  '密钥类型：user=用户自建，system=系统托管（如生图工作台）';
