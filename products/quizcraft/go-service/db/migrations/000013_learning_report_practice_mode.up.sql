-- Extend pinned practice sessions with the set recommended by an evidence-based
-- learning report. Expand only: existing sessions keep their mode, and the
-- report route still revalidates published content before pinning questions.
DO $$ BEGIN
    ALTER TABLE quizcraft_practice_sessions
        DROP CONSTRAINT IF EXISTS quizcraft_practice_sessions_mode_check;
    ALTER TABLE quizcraft_practice_sessions
        ADD CONSTRAINT quizcraft_practice_sessions_mode_check
        CHECK (mode IN ('random','difficult','chapter','favorites','report'));
END $$;
