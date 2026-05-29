ALTER TABLE servers
    DROP COLUMN IF EXISTS country,
    DROP COLUMN IF EXISTS country_code,
    DROP COLUMN IF EXISTS remote_public_ip;
