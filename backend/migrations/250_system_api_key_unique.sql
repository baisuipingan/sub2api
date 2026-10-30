-- 系统托管密钥的唯一性约束。
--
-- 背景：GetOrCreateImageStudioKey 采用「先查后插」，并发下（用户同时开两个标签页、
-- 或前端重复请求）两个请求可能都查到 nil 再各自插入，结果同一用户出现两把
-- 「生图工作台专用」密钥。多出来的那把用户无法删除（系统密钥受保护），
-- 会一直挂在密钥列表里。
--
-- 因此把幂等性下沉到数据库：同一用户 + 同一用途 + 未删除，只能有一条。

-- 加固前先清理历史脏数据：每组保留 id 最小的一条，其余软删除。
-- （用软删除而非物理删除，保留审计痕迹，也让 key 记录的 deleted_at 语义一致）
WITH ranked AS (
  SELECT id,
         ROW_NUMBER() OVER (PARTITION BY user_id, name ORDER BY id) AS rn
  FROM api_keys
  WHERE key_type = 'system'
    AND deleted_at IS NULL
)
UPDATE api_keys
SET deleted_at = NOW(),
    updated_at = NOW()
WHERE id IN (SELECT id FROM ranked WHERE rn > 1);

-- 部分唯一索引：只约束「未删除的系统密钥」，不影响用户自建密钥，
-- 也不影响系统密钥被软删除后重新创建。
CREATE UNIQUE INDEX IF NOT EXISTS uq_api_keys_system_key_per_user
  ON api_keys (user_id, name)
  WHERE key_type = 'system' AND deleted_at IS NULL;

COMMENT ON INDEX uq_api_keys_system_key_per_user IS
  '同一用户的系统托管密钥（按用途 name）唯一，防止并发重复创建';
