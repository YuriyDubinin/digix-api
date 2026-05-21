DROP TRIGGER IF EXISTS set_updated_at_feedback_requests ON feedback_requests;
DROP FUNCTION IF EXISTS trigger_set_updated_at();
DROP TABLE IF EXISTS feedback_requests;
