ALTER TABLE connector_apis ADD COLUMN group_id BIGINT REFERENCES tool_groups(id) ON DELETE SET NULL;

CREATE INDEX idx_connector_apis_group ON connector_apis(group_id);
