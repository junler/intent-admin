-- +goose Up
-- User-requested irreversible cleanup of the retired change/review history.
-- Business rows, accounts, credentials, sessions and ordinary task/login logs remain.
SELECT pg_advisory_xact_lock(745186203);

DELETE FROM sys_ai_plan_approval;
DELETE FROM sys_changeset_item;
DELETE FROM sys_changeset;
DELETE FROM sys_oper_log;
DELETE FROM sys_notification
WHERE link LIKE '/ai/approvals%' OR link LIKE '/changesets%' OR link LIKE '/audit%';

UPDATE sys_ai_plan
SET page_context = page_context - 'changesetId' - 'approvalId'
WHERE page_context ? 'changesetId' OR page_context ? 'approvalId';

UPDATE sys_job SET status='1'
WHERE invoke_target='system:cleanChangeset' AND del_flag='0';

-- +goose Down
-- Deleted history cannot be reconstructed by a schema downgrade.
SELECT 1;
