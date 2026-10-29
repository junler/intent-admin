-- +goose Up
ALTER TABLE sys_changeset ALTER COLUMN status TYPE varchar(32);
ALTER TABLE sys_changeset ADD COLUMN rollback_base_status varchar(32) NOT NULL DEFAULT '';
ALTER TABLE sys_changeset_item
    ADD COLUMN error_code int NOT NULL DEFAULT 0,
    ADD COLUMN conflict_fields jsonb,
    ADD COLUMN rolled_back_at timestamptz,
    ADD COLUMN rolled_back_by varchar(64) NOT NULL DEFAULT '',
    ADD COLUMN rollback_changeset_id varchar(36) REFERENCES sys_changeset(changeset_id) ON DELETE SET NULL;
CREATE INDEX idx_sys_changeset_rollback_of ON sys_changeset (rollback_of, create_time);
CREATE INDEX idx_sys_changeset_item_rollback ON sys_changeset_item (rollback_changeset_id) WHERE rollback_changeset_id IS NOT NULL;

-- +goose Down
DROP INDEX IF EXISTS idx_sys_changeset_item_rollback;
DROP INDEX IF EXISTS idx_sys_changeset_rollback_of;
ALTER TABLE sys_changeset_item DROP COLUMN rollback_changeset_id, DROP COLUMN rolled_back_by, DROP COLUMN rolled_back_at, DROP COLUMN conflict_fields, DROP COLUMN error_code;
ALTER TABLE sys_changeset DROP COLUMN rollback_base_status;
UPDATE sys_changeset SET status='rollback_failed' WHERE status='partially_rolled_back';
ALTER TABLE sys_changeset ALTER COLUMN status TYPE varchar(20);
