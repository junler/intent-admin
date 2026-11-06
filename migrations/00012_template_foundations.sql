-- +goose Up
-- Versions advance for every writer, including grants and maintenance paths.
-- +goose StatementBegin
CREATE FUNCTION increment_record_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.version := OLD.version + 1;
    RETURN NEW;
END;
$$;
DO $$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['sys_user','sys_role','sys_menu','sys_dict_type','sys_dict_data','sys_config','sys_notice','demo_item'] LOOP
        EXECUTE format('ALTER TABLE %I ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0)', table_name);
        EXECUTE format('CREATE TRIGGER record_version BEFORE UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION increment_record_version()', table_name);
    END LOOP;
END;
$$;
-- +goose StatementEnd

ALTER TABLE sys_user ADD COLUMN must_change_password boolean NOT NULL DEFAULT false;

CREATE TABLE sys_request_receipt (
    request_id char(64) PRIMARY KEY,
    input_digest char(64) NOT NULL,
    result jsonb NOT NULL,
    expires_at timestamptz NOT NULL
);
CREATE INDEX sys_request_receipt_expiry ON sys_request_receipt(expires_at);

-- Each business file is claimed by one record; access follows that record.
CREATE TABLE sys_attachment (
    entity varchar(100) NOT NULL,
    record_id varchar(100) NOT NULL,
    upload_id varchar(36) NOT NULL REFERENCES sys_upload(upload_id),
    create_time timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (entity, record_id, upload_id)
);
CREATE UNIQUE INDEX sys_attachment_file ON sys_attachment(upload_id);

-- +goose Down
DROP TABLE sys_attachment;
DROP TABLE sys_request_receipt;
ALTER TABLE sys_user DROP COLUMN must_change_password;
-- +goose StatementBegin
DO $$
DECLARE table_name text;
BEGIN
    FOREACH table_name IN ARRAY ARRAY['sys_user','sys_role','sys_menu','sys_dict_type','sys_dict_data','sys_config','sys_notice','demo_item'] LOOP
        EXECUTE format('DROP TRIGGER record_version ON %I', table_name);
        EXECUTE format('ALTER TABLE %I DROP COLUMN version', table_name);
    END LOOP;
END;
$$;
-- +goose StatementEnd
DROP FUNCTION increment_record_version();
