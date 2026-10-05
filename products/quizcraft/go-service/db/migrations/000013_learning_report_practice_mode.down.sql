-- Development rollback drops the report-pinned sessions: they cannot be resumed
-- without the mode, and their questions are derived data. Other sessions and the
-- original attempts are not touched.
DO $$ BEGIN
    DELETE FROM quizcraft_practice_session_questions
     WHERE session_id IN (SELECT id FROM quizcraft_practice_sessions WHERE mode = 'report');
    DELETE FROM quizcraft_practice_sessions WHERE mode = 'report';
    ALTER TABLE quizcraft_practice_sessions
        DROP CONSTRAINT IF EXISTS quizcraft_practice_sessions_mode_check;
    ALTER TABLE quizcraft_practice_sessions
        ADD CONSTRAINT quizcraft_practice_sessions_mode_check
        CHECK (mode IN ('random','difficult','chapter','favorites'));
EXCEPTION WHEN undefined_table THEN NULL;
END $$;
DO $$ BEGIN
    DELETE FROM quizcraft_schema_migrations WHERE version = '000013';
EXCEPTION WHEN undefined_table THEN NULL;
END $$;
