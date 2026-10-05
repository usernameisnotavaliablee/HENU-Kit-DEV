-- Minimal QuizCraft fixture for the joint Gateway <-> real-Core member
-- learning-report chain. It is applied once, by
-- internal/httpapi/learning_report_joint_test.go, to the freshly created
-- quizcraft_v2 database after db/migrations has been applied twice.
--
-- It seeds exactly what a member learning-report chain touches: one published
-- course with a sealed bank version, two single-choice questions in one
-- chapter, one approved learning content version wired to the active catalog,
-- and one answered attempt for the member who walks the chain. Preferences are
-- deliberately absent: the chain must prove that a member with no stored
-- preferences reads the Core's own disabled defaults instead of a fixture.
--
-- psql variables, all supplied by the test so the fixture has one source of
-- truth for every identity:
--   bank_id bank_version_id content_version_id reviewer_id
--   question_id_1 question_version_id_1 question_id_2 question_version_id_2
--   session_id attempt_id chain_user_id document

INSERT INTO quizcraft_banks(id, bank_key, name)
VALUES (:'bank_id'::uuid, 'joint-learning-chain', '联合链路题库');

INSERT INTO quizcraft_bank_versions(id, bank_id, name, source_version, source_sha256, content_sha256, import_report)
VALUES (:'bank_version_id'::uuid, :'bank_id'::uuid, '联合链路题库 v1', 'joint-v1', repeat('a', 64), repeat('b', 64), '{}'::jsonb);

INSERT INTO quizcraft_questions(id, bank_id, source_question_id) VALUES
    (:'question_id_1'::uuid, :'bank_id'::uuid, 'jq0001'),
    (:'question_id_2'::uuid, :'bank_id'::uuid, 'jq0002');

INSERT INTO quizcraft_question_versions(id, bank_id, question_id, type, chapter_id, chapter_name, content, options, answer, analysis, content_sha256) VALUES
    (:'question_version_id_1'::uuid, :'bank_id'::uuid, :'question_id_1'::uuid, 'single', 'ch01', '基础运算', '1+1=?', '["0","1","2"]'::jsonb, '1'::jsonb, '', repeat('c', 64)),
    (:'question_version_id_2'::uuid, :'bank_id'::uuid, :'question_id_2'::uuid, 'single', 'ch01', '基础运算', '2+2=?', '["3","4","5"]'::jsonb, '1'::jsonb, '', repeat('d', 64));

-- A sealed bank version accepts no new curriculum, so membership is written
-- first and the version is sealed afterwards; publishing the course is then a
-- separate pointer update.
INSERT INTO quizcraft_bank_version_questions(bank_id, bank_version_id, question_id, question_version_id, position) VALUES
    (:'bank_id'::uuid, :'bank_version_id'::uuid, :'question_id_1'::uuid, :'question_version_id_1'::uuid, 1),
    (:'bank_id'::uuid, :'bank_version_id'::uuid, :'question_id_2'::uuid, :'question_version_id_2'::uuid, 2);

UPDATE quizcraft_bank_versions SET sealed_at = now() WHERE id = :'bank_version_id'::uuid;
UPDATE quizcraft_banks SET active_version_id = :'bank_version_id'::uuid, updated_at = now() WHERE id = :'bank_id'::uuid;

-- The Core re-derives the digest from the stored document with Go's sha256 over
-- the canonical re-marshal, so the fixture hashes exactly the bytes the test
-- hands over instead of a hand-copied hex constant.
INSERT INTO quizcraft_learning_content_versions(id, bank_id, bank_version_id, content_sha256, document, status, reviewed_by, reviewed_at)
VALUES (:'content_version_id'::uuid, :'bank_id'::uuid, :'bank_version_id'::uuid,
        encode(sha256(convert_to(:'document', 'UTF8')), 'hex'), :'document'::jsonb, 'approved', :'reviewer_id'::uuid, now());

INSERT INTO quizcraft_learning_catalogs(bank_id, active_content_version_id, enabled)
VALUES (:'bank_id'::uuid, :'content_version_id'::uuid, true);

-- One answered question for the chain member, so the queued report is built
-- from a real immutable attempt rather than a cold start.
INSERT INTO quizcraft_practice_sessions(id, bank_id, bank_version_id, user_id, actor_key, mode)
VALUES (:'session_id'::uuid, :'bank_id'::uuid, :'bank_version_id'::uuid, :'chain_user_id'::uuid, 'user:' || :'chain_user_id', 'random');

INSERT INTO quizcraft_practice_session_questions(session_id, bank_id, bank_version_id, question_id, question_version_id, position)
VALUES (:'session_id'::uuid, :'bank_id'::uuid, :'bank_version_id'::uuid, :'question_id_1'::uuid, :'question_version_id_1'::uuid, 1);

INSERT INTO quizcraft_practice_attempts(id, session_id, bank_id, bank_version_id, question_id, question_version_id, user_id, submitted_answer, correct, expected_answer, response_body)
VALUES (:'attempt_id'::uuid, :'session_id'::uuid, :'bank_id'::uuid, :'bank_version_id'::uuid, :'question_id_1'::uuid, :'question_version_id_1'::uuid, :'chain_user_id'::uuid, '0'::jsonb, false, '1'::jsonb, '{}');
