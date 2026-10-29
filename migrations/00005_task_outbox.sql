-- +goose Up
-- Durable handoff between PostgreSQL transactions and the Redis task queue.
-- A stable task ID and a row-locked completion marker make redelivery safe.
CREATE TABLE sys_task_outbox (
    task_id varchar(36) PRIMARY KEY,
    task_type varchar(100) NOT NULL,
    payload jsonb NOT NULL,
    queue_name varchar(100) NOT NULL,
    max_retry int NOT NULL DEFAULT 0,
    timeout_seconds bigint NOT NULL DEFAULT 1800,
    unique_seconds bigint NOT NULL DEFAULT 0,
    dispatch_state varchar(16) NOT NULL DEFAULT 'pending' CHECK (dispatch_state IN ('pending','delivered')),
    execution_state varchar(16) NOT NULL DEFAULT 'pending' CHECK (execution_state IN ('pending','running','done','failed')),
    attempts int NOT NULL DEFAULT 0,
    last_error text NOT NULL DEFAULT '',
    next_attempt_at timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz,
    completed_at timestamptz,
    create_time timestamptz NOT NULL DEFAULT now(),
    update_time timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_task_outbox_pending ON sys_task_outbox (next_attempt_at, create_time) WHERE dispatch_state='pending' AND execution_state<>'done';
CREATE INDEX idx_task_outbox_queue ON sys_task_outbox (queue_name, create_time DESC);

-- +goose Down
DROP TABLE IF EXISTS sys_task_outbox;
