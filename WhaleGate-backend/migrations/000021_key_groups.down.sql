-- 回滚分组功能：删除分组表。
-- api_keys.group_tag 保留原样（它是加字段前就存在的列），
-- 只是重新回到"纯文本标签、不参与计费与限流"的行为。
DROP INDEX IF EXISTS idx_key_groups_name_active;
DROP INDEX IF EXISTS idx_key_groups_deleted_at;
DROP TABLE IF EXISTS key_groups;
