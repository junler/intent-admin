-- +goose Up
-- Keep historical records and role references while retiring these product surfaces.
-- Seed reruns apply this script last, so older seeds cannot restore retired menus.
SELECT pg_advisory_xact_lock(745186203);

UPDATE sys_menu SET status='1'
WHERE del_flag='0' AND (
  path IN ('/changes','/changesets','/audit','/ai/approvals')
  OR component IN ('changesets/index','audit/index','ai/approvals/index')
  OR perms LIKE 'system:changeset:%'
  OR perms='ai:plan:approve'
);

-- +goose Down
-- Restoring retired endpoints requires an application downgrade as well.
SELECT 1;
