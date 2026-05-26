CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

CREATE TYPE feedback_status AS ENUM (
    'NEW',
    'PROCESSING',
    'CLOSED'
);

CREATE TABLE feedback_requests (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name       VARCHAR(255) NOT NULL,
    email      VARCHAR(255) NOT NULL,
    phone      VARCHAR(50),
    subject    VARCHAR(500),
    message    TEXT NOT NULL,
    status     feedback_status NOT NULL DEFAULT 'NEW',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_feedback_requests_status_created_at
    ON feedback_requests (status, created_at DESC);

CREATE INDEX idx_feedback_requests_email
    ON feedback_requests (email);

CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER set_updated_at_feedback_requests
    BEFORE UPDATE ON feedback_requests
    FOR EACH ROW
    EXECUTE FUNCTION trigger_set_updated_at();
