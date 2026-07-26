ALTER TABLE tenants DROP CONSTRAINT tenants_domain_key;

ALTER TABLE users DROP CONSTRAINT users_email_key;
ALTER TABLE users ADD CONSTRAINT users_email_org_unique UNIQUE (email, organization_id);
