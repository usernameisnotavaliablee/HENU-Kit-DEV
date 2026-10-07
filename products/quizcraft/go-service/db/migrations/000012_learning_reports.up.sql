-- Learning Reports own derived data only. Existing attempts and mastery are unchanged.
CREATE TABLE IF NOT EXISTS quizcraft_learning_content_versions (
    id uuid PRIMARY KEY,
    bank_id uuid NOT NULL,
    bank_version_id uuid NOT NULL,
    content_sha256 text NOT NULL CHECK (content_sha256 ~ '^[0-9a-f]{64}$'),
    document jsonb NOT NULL CHECK (jsonb_typeof(document)='object' AND document ? 'schema_version' AND document->>'schema_version'='1'),
    status text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft','approved','retired')),
    reviewed_by uuid,
    reviewed_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(bank_id,id),
    UNIQUE(bank_id,content_sha256),
    FOREIGN KEY(bank_id,bank_version_id) REFERENCES quizcraft_bank_versions(bank_id,id),
    CHECK ((status='draft' AND reviewed_by IS NULL AND reviewed_at IS NULL) OR
           (status IN ('approved','retired') AND reviewed_by IS NOT NULL AND reviewed_at IS NOT NULL))
);

CREATE TABLE IF NOT EXISTS quizcraft_learning_catalogs (
    bank_id uuid PRIMARY KEY REFERENCES quizcraft_banks(id),
    active_content_version_id uuid,
    enabled boolean NOT NULL DEFAULT false,
    updated_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(bank_id,active_content_version_id) REFERENCES quizcraft_learning_content_versions(bank_id,id),
    CHECK (NOT enabled OR active_content_version_id IS NOT NULL)
);

CREATE TABLE IF NOT EXISTS quizcraft_learning_content_reviews (
    id uuid PRIMARY KEY,
    bank_id uuid NOT NULL,
    content_version_id uuid NOT NULL,
    actor_user_id uuid NOT NULL,
    action text NOT NULL CHECK (action IN ('approve','retire','publish','unpublish')),
    note text NOT NULL DEFAULT '' CHECK (char_length(note)<=2000),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(bank_id,content_version_id) REFERENCES quizcraft_learning_content_versions(bank_id,id)
);

CREATE TABLE IF NOT EXISTS quizcraft_learning_report_preferences (
    user_id uuid NOT NULL,
    bank_id uuid NOT NULL REFERENCES quizcraft_banks(id),
    enabled boolean NOT NULL DEFAULT false,
    interval_days integer NOT NULL DEFAULT 7 CHECK (interval_days BETWEEN 1 AND 30),
    goal text NOT NULL DEFAULT 'follow_course' CHECK (goal IN ('follow_course','exam_review')),
    chapter_ids jsonb NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(chapter_ids)='array'),
    external_analysis_consent boolean NOT NULL DEFAULT false,
    consent_version text NOT NULL DEFAULT '' CHECK (char_length(consent_version)<=160),
    revision bigint NOT NULL DEFAULT 1 CHECK (revision>0),
    next_due_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY(user_id,bank_id),
    CHECK (NOT enabled OR (external_analysis_consent AND consent_version<>'' AND next_due_at IS NOT NULL))
);
CREATE INDEX IF NOT EXISTS quizcraft_learning_preferences_due_idx
    ON quizcraft_learning_report_preferences(next_due_at) WHERE enabled;

CREATE TABLE IF NOT EXISTS quizcraft_learning_report_jobs (
    id uuid PRIMARY KEY,
    user_id uuid NOT NULL,
    bank_id uuid NOT NULL,
    preference_revision bigint NOT NULL CHECK (preference_revision>0),
    content_version_id uuid NOT NULL,
    input_sha256 text NOT NULL CHECK (input_sha256 ~ '^[0-9a-f]{64}$'),
    snapshot jsonb NOT NULL CHECK (jsonb_typeof(snapshot)='object'),
    status text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued','running','ready','failed','paused','cancelled')),
    run_after timestamptz NOT NULL DEFAULT now(),
    attempts integer NOT NULL DEFAULT 0 CHECK (attempts>=0),
    lease_token uuid,
    lease_until timestamptz,
    reason_code text NOT NULL DEFAULT '' CHECK (char_length(reason_code)<=80),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE(user_id,bank_id,preference_revision,input_sha256),
    UNIQUE(id,user_id,bank_id,content_version_id),
    FOREIGN KEY(user_id,bank_id) REFERENCES quizcraft_learning_report_preferences(user_id,bank_id) ON DELETE CASCADE,
    FOREIGN KEY(bank_id,content_version_id) REFERENCES quizcraft_learning_content_versions(bank_id,id),
    CHECK ((status='running' AND lease_token IS NOT NULL AND lease_until IS NOT NULL) OR
           (status<>'running' AND lease_token IS NULL AND lease_until IS NULL))
);
CREATE INDEX IF NOT EXISTS quizcraft_learning_jobs_due_idx
    ON quizcraft_learning_report_jobs(run_after,created_at) WHERE status='queued';
CREATE INDEX IF NOT EXISTS quizcraft_learning_jobs_lease_idx
    ON quizcraft_learning_report_jobs(lease_until) WHERE status='running';

CREATE TABLE IF NOT EXISTS quizcraft_learning_reports (
    id uuid PRIMARY KEY,
    job_id uuid NOT NULL UNIQUE,
    user_id uuid NOT NULL,
    bank_id uuid NOT NULL,
    content_version_id uuid NOT NULL,
    lease_token uuid NOT NULL,
    evidence_until timestamptz NOT NULL,
    status text NOT NULL CHECK (status IN ('ready','insufficient_evidence','stale')),
    body jsonb NOT NULL CHECK (jsonb_typeof(body)='object'),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY(job_id,user_id,bank_id,content_version_id)
        REFERENCES quizcraft_learning_report_jobs(id,user_id,bank_id,content_version_id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS quizcraft_learning_reports_latest_idx
    ON quizcraft_learning_reports(user_id,bank_id,created_at DESC);

CREATE OR REPLACE FUNCTION quizcraft_guard_learning_content_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id,NEW.bank_id,NEW.bank_version_id,NEW.created_at) IS DISTINCT FROM
       ROW(OLD.id,OLD.bank_id,OLD.bank_version_id,OLD.created_at) THEN
        RAISE EXCEPTION 'learning content identity is immutable' USING ERRCODE='23514';
    END IF;
    IF OLD.status<>'draft' AND
       ROW(NEW.document,NEW.content_sha256,NEW.reviewed_by,NEW.reviewed_at) IS DISTINCT FROM
       ROW(OLD.document,OLD.content_sha256,OLD.reviewed_by,OLD.reviewed_at) THEN
        RAISE EXCEPTION 'reviewed learning content is immutable' USING ERRCODE='23514';
    END IF;
    IF (OLD.status='draft' AND NEW.status NOT IN ('draft','approved')) OR
       (OLD.status='approved' AND NEW.status NOT IN ('approved','retired')) OR
       (OLD.status='retired' AND NEW.status<>'retired') THEN
        RAISE EXCEPTION 'invalid learning content transition' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
DO $$ BEGIN
    CREATE TRIGGER quizcraft_learning_content_guard
        BEFORE UPDATE ON quizcraft_learning_content_versions
        FOR EACH ROW EXECUTE FUNCTION quizcraft_guard_learning_content_update();
EXCEPTION WHEN duplicate_object THEN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION quizcraft_guard_learning_report_publish() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP='UPDATE' THEN
        IF NEW.status<>'stale' OR
           (to_jsonb(NEW)-'status') IS DISTINCT FROM (to_jsonb(OLD)-'status') THEN
            RAISE EXCEPTION 'published learning report is immutable' USING ERRCODE='23514';
        END IF;
        RETURN NEW;
    END IF;
    -- A short publication transaction locks revocable inputs, never a model call.
    -- Cleanup locks preferences before deleting derived rows. Whichever wins,
    -- a result captured before consent revocation cannot survive cleanup.
    PERFORM 1
    FROM quizcraft_learning_report_jobs j
    JOIN quizcraft_learning_report_preferences p ON p.user_id=j.user_id AND p.bank_id=j.bank_id
    JOIN quizcraft_learning_content_versions c ON c.bank_id=j.bank_id AND c.id=j.content_version_id
    JOIN quizcraft_learning_catalogs l ON l.bank_id=c.bank_id AND l.active_content_version_id=c.id
    JOIN quizcraft_banks b ON b.id=c.bank_id AND b.active_version_id=c.bank_version_id
    WHERE j.id=NEW.job_id AND j.user_id=NEW.user_id AND j.bank_id=NEW.bank_id
      AND j.content_version_id=NEW.content_version_id
      AND j.status='running' AND j.lease_token=NEW.lease_token AND j.lease_until>clock_timestamp()
      AND p.enabled AND p.external_analysis_consent AND p.revision=j.preference_revision
      AND c.status='approved' AND l.enabled
    FOR SHARE OF p,j,c,l,b;
    IF NOT FOUND THEN
        RAISE EXCEPTION 'learning report inputs were revoked, superseded or expired' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
DO $$ BEGIN
    CREATE TRIGGER quizcraft_learning_report_publish_guard
        BEFORE INSERT OR UPDATE ON quizcraft_learning_reports
        FOR EACH ROW EXECUTE FUNCTION quizcraft_guard_learning_report_publish();
EXCEPTION WHEN duplicate_object THEN NULL;
END;
$$;

DO $$ BEGIN
    CREATE TRIGGER quizcraft_learning_content_reviews_immutable
        BEFORE UPDATE OR DELETE OR TRUNCATE ON quizcraft_learning_content_reviews
        EXECUTE FUNCTION quizcraft_reject_immutable_mutation();
EXCEPTION WHEN duplicate_object THEN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION quizcraft_guard_learning_job_update() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF ROW(NEW.id,NEW.user_id,NEW.bank_id,NEW.preference_revision,NEW.content_version_id,NEW.input_sha256,NEW.snapshot,NEW.created_at)
       IS DISTINCT FROM
       ROW(OLD.id,OLD.user_id,OLD.bank_id,OLD.preference_revision,OLD.content_version_id,OLD.input_sha256,OLD.snapshot,OLD.created_at) THEN
        RAISE EXCEPTION 'learning job evidence and consent generation are immutable' USING ERRCODE='23514';
    END IF;
    RETURN NEW;
END;
$$;
DO $$ BEGIN
    CREATE TRIGGER quizcraft_learning_job_input_guard
        BEFORE UPDATE ON quizcraft_learning_report_jobs
        FOR EACH ROW EXECUTE FUNCTION quizcraft_guard_learning_job_update();
EXCEPTION WHEN duplicate_object THEN NULL;
END;
$$;
