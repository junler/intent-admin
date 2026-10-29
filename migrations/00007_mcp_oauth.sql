-- +goose Up
-- OAuth credentials are random opaque values; only SHA-256 digests are stored.
CREATE TABLE sys_oauth_client (
    client_id varchar(36) PRIMARY KEY,
    client_name varchar(100) NOT NULL,
    redirect_uris jsonb NOT NULL,
    create_time timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sys_oauth_grant (
    grant_id bigserial PRIMARY KEY,
    client_id varchar(36) NOT NULL REFERENCES sys_oauth_client(client_id),
    user_id bigint NOT NULL REFERENCES sys_user(user_id),
    scopes jsonb NOT NULL,
    permissions jsonb NOT NULL,
    resource text NOT NULL,
    expires_at timestamptz NOT NULL,
    revoked_at timestamptz,
    create_time timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_oauth_grant_owner ON sys_oauth_grant(user_id, create_time DESC);
CREATE TABLE sys_oauth_request (
    request_id varchar(36) PRIMARY KEY,
    client_id varchar(36) NOT NULL REFERENCES sys_oauth_client(client_id),
    redirect_uri text NOT NULL,
    state text NOT NULL DEFAULT '',
    challenge varchar(43) NOT NULL,
    scopes jsonb NOT NULL,
    resource text NOT NULL,
    user_id bigint,
    status varchar(16) NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','approved','denied','consumed')),
    code_hash varchar(64),
    grant_id bigint REFERENCES sys_oauth_grant(grant_id),
    expires_at timestamptz NOT NULL,
    create_time timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX idx_oauth_code_hash ON sys_oauth_request(code_hash) WHERE code_hash IS NOT NULL;
CREATE INDEX idx_oauth_request_expiry ON sys_oauth_request(expires_at);
CREATE TABLE sys_oauth_token (
    token_id bigserial PRIMARY KEY,
    grant_id bigint NOT NULL REFERENCES sys_oauth_grant(grant_id),
    access_hash varchar(64) UNIQUE NOT NULL,
    refresh_hash varchar(64) UNIQUE,
    access_expires_at timestamptz NOT NULL,
    refresh_expires_at timestamptz,
    refresh_used_at timestamptz,
    revoked_at timestamptz,
    create_time timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX idx_oauth_token_grant ON sys_oauth_token(grant_id);

-- +goose Down
DROP TABLE IF EXISTS sys_oauth_token;
DROP TABLE IF EXISTS sys_oauth_request;
DROP TABLE IF EXISTS sys_oauth_grant;
DROP TABLE IF EXISTS sys_oauth_client;
