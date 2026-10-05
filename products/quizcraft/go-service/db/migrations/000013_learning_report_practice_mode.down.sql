-- Development rollback narrows the mode check back to the pre-feature values. It
-- deliberately does NOT delete report-mode sessions: their questions sit behind a
-- statement-level immutability trigger, and destroying member rows to make a
-- schema rollback tidy is not a trade this schema makes. When such rows exist the
-- widened check is left in place, which the previous code tolerates because it
-- never writes 'report'. The history row is always removed so this file stays
-- re-runnable.
DO $$ BEGIN
    IF NOT EXISTS (SELECT 1 FROM quizcraft_practice_sessions WHERE mode = 'report') THEN
        ALTER TABLE quizcraft_practice_sessions
            DROP CONSTRAINT IF EXISTS quizcraft_practice_sessions_mode_check;
        ALTER TABLE quizcraft_practice_sessions
            ADD CONSTRAINT quizcraft_practice_sessions_mode_check
            CHECK (mode IN ('random','difficult','chapter','favorites'));
    END IF;
EXCEPTION WHEN undefined_table THEN NULL;
END $$;
DO $$ BEGIN
    DELETE FROM quizcraft_schema_migrations WHERE version = '000013';
EXCEPTION WHEN undefined_table THEN NULL;
END $$;
