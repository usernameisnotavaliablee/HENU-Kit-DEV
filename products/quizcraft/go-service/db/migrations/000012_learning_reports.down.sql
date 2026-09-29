-- Development rollback removes only this feature's derived data.
-- Export reviewed content and reports before an intentional production rollback.
DROP TABLE IF EXISTS quizcraft_learning_reports;
DROP TABLE IF EXISTS quizcraft_learning_report_jobs;
DROP TABLE IF EXISTS quizcraft_learning_report_preferences;
DROP TABLE IF EXISTS quizcraft_learning_content_reviews;
DROP TABLE IF EXISTS quizcraft_learning_catalogs;
DROP TABLE IF EXISTS quizcraft_learning_content_versions;
DROP FUNCTION IF EXISTS quizcraft_guard_learning_report_publish();
DROP FUNCTION IF EXISTS quizcraft_guard_learning_job_update();
DROP FUNCTION IF EXISTS quizcraft_guard_learning_content_update();
DO $$ BEGIN
    DELETE FROM quizcraft_schema_migrations WHERE version='000012';
EXCEPTION WHEN undefined_table THEN NULL;
END;
$$;
