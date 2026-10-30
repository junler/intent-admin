-- +goose Up
-- An approval is bound to one immutable preview and one distinct reviewer.
CREATE TABLE sys_ai_plan_approval (
    approval_id varchar(36) PRIMARY KEY,
    plan_id varchar(36) NOT NULL REFERENCES sys_ai_plan(plan_id),
    proposer_id bigint NOT NULL,
    proposer_name varchar(64) NOT NULL,
    reviewer_id bigint NOT NULL,
    reviewer_name varchar(64) NOT NULL,
    status varchar(16) NOT NULL CHECK (status IN ('pending','approved','rejected','revoked','expired','stale','applied')),
    fingerprint varchar(64) NOT NULL,
    snapshot jsonb NOT NULL,
    request_comment varchar(1000) NOT NULL DEFAULT '',
    decision_comment varchar(1000) NOT NULL DEFAULT '',
    create_time timestamptz NOT NULL DEFAULT now(),
    expire_time timestamptz NOT NULL,
    decision_time timestamptz,
    update_time timestamptz NOT NULL DEFAULT now(),
    changeset_id varchar(36) NOT NULL DEFAULT '',
    CHECK (proposer_id <> reviewer_id)
);
CREATE UNIQUE INDEX idx_plan_approval_active ON sys_ai_plan_approval(plan_id) WHERE status IN ('pending','approved');
CREATE INDEX idx_plan_approval_inbox ON sys_ai_plan_approval(reviewer_id,status,create_time DESC);
CREATE INDEX idx_plan_approval_sent ON sys_ai_plan_approval(proposer_id,create_time DESC);

-- +goose Down
DROP TABLE IF EXISTS sys_ai_plan_approval;
