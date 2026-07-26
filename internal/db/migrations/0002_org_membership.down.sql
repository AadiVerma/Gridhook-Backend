ALTER TABLE users DROP CONSTRAINT users_email_org_unique;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);

ALTER TABLE tenants ADD CONSTRAINT tenants_domain_key UNIQUE (domain);
