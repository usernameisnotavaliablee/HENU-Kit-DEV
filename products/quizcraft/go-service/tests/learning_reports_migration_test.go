package tests

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	quizcraft "henukit.dev/quizcraft"
)

func TestLearningReportStorageConsentAndPublicationGuards(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-report-storage")
	user, content, job, lease := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	reject := func(query string, args ...any) {
		t.Helper()
		_, err := pool.Exec(ctx, query, args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || (pgErr.Code != "23514" && pgErr.Code != "23503") {
			t.Fatalf("expected check/foreign-key rejection, got %v", err)
		}
	}
	exec(`INSERT INTO quizcraft_learning_report_preferences(user_id,bank_id) VALUES($1,$2)`, user, bank.BankID)
	var enabled, consent bool
	var days int
	if err := pool.QueryRow(ctx, `SELECT enabled,external_analysis_consent,interval_days FROM quizcraft_learning_report_preferences WHERE user_id=$1 AND bank_id=$2`, user, bank.BankID).Scan(&enabled, &consent, &days); err != nil {
		t.Fatal(err)
	}
	if enabled || consent || days != 7 {
		t.Fatalf("unsafe defaults: %v %v %d", enabled, consent, days)
	}
	for _, value := range []int{0, 31} {
		reject(`UPDATE quizcraft_learning_report_preferences SET interval_days=$3 WHERE user_id=$1 AND bank_id=$2`, user, bank.BankID, value)
	}
	reject(`UPDATE quizcraft_learning_report_preferences SET enabled=true WHERE user_id=$1 AND bank_id=$2`, user, bank.BankID)
	exec(`INSERT INTO quizcraft_learning_content_versions(id,bank_id,bank_version_id,content_sha256,document) VALUES($1,$2,$3,repeat('a',64),'{"schema_version":1}')`, content, bank.BankID, bank.BankVersionID)
	reject(`UPDATE quizcraft_learning_content_versions SET status='approved' WHERE id=$1`, content)
	exec(`UPDATE quizcraft_learning_content_versions SET status='approved',reviewed_by=$2,reviewed_at=now() WHERE id=$1`, content, user)
	reject(`UPDATE quizcraft_learning_content_versions SET document='{"schema_version":1,"edited":true}' WHERE id=$1`, content)
	exec(`INSERT INTO quizcraft_learning_catalogs(bank_id,active_content_version_id,enabled) VALUES($1,$2,true)`, bank.BankID, content)
	exec(`UPDATE quizcraft_learning_report_preferences SET enabled=true,external_analysis_consent=true,consent_version='v1',next_due_at=now()+interval '7 days' WHERE user_id=$1 AND bank_id=$2`, user, bank.BankID)
	exec(`INSERT INTO quizcraft_learning_report_jobs(id,user_id,bank_id,preference_revision,content_version_id,input_sha256,snapshot,status,lease_token,lease_until) VALUES($1,$2,$3,1,$4,repeat('b',64),'{}','running',$5,now()+interval '5 minutes')`, job, user, bank.BankID, content, lease)
	reject(`UPDATE quizcraft_learning_report_jobs SET preference_revision=2 WHERE id=$1`, job)
	reject(`UPDATE quizcraft_learning_report_jobs SET snapshot='{"tampered":true}' WHERE id=$1`, job)
	publish := `INSERT INTO quizcraft_learning_reports(id,job_id,user_id,bank_id,content_version_id,lease_token,evidence_until,status,body) VALUES($1,$2,$3,$4,$5,$6,now(),'ready','{}')`
	reject(publish, uuid.New(), job, uuid.New(), bank.BankID, content, lease)
	otherBank := importPracticeBank(t, pool, "learning-report-other")
	reject(publish, uuid.New(), job, user, otherBank.BankID, content, lease)
	reject(publish, uuid.New(), job, user, bank.BankID, content, uuid.New())
	exec(`UPDATE quizcraft_learning_report_preferences SET revision=revision+1,enabled=false,external_analysis_consent=false,next_due_at=NULL WHERE user_id=$1 AND bank_id=$2`, user, bank.BankID)
	reject(publish, uuid.New(), job, user, bank.BankID, content, lease)
	// Re-enabling consent must not revive a job captured before revocation.
	exec(`UPDATE quizcraft_learning_report_preferences SET enabled=true,external_analysis_consent=true,next_due_at=now()+interval '7 days' WHERE user_id=$1 AND bank_id=$2`, user, bank.BankID)
	reject(publish, uuid.New(), job, user, bank.BankID, content, lease)
	fresh := uuid.New()
	exec(`INSERT INTO quizcraft_learning_report_jobs(id,user_id,bank_id,preference_revision,content_version_id,input_sha256,snapshot,status,lease_token,lease_until) VALUES($1,$2,$3,2,$4,repeat('b',64),'{}','running',$5,now()+interval '5 minutes')`, fresh, user, bank.BankID, content, lease)
	exec(`UPDATE quizcraft_learning_report_jobs SET lease_until=now()-interval '1 second' WHERE id=$1`, fresh)
	reject(publish, uuid.New(), fresh, user, bank.BankID, content, lease)
	exec(`UPDATE quizcraft_learning_report_jobs SET lease_until=now()+interval '5 minutes' WHERE id=$1`, fresh)
	exec(`UPDATE quizcraft_learning_catalogs SET enabled=false WHERE bank_id=$1`, bank.BankID)
	reject(publish, uuid.New(), fresh, user, bank.BankID, content, lease)
	exec(`UPDATE quizcraft_learning_catalogs SET enabled=true WHERE bank_id=$1`, bank.BankID)
	exec(publish, uuid.New(), fresh, user, bank.BankID, content, lease)
	exec(`UPDATE quizcraft_learning_content_versions SET status='retired' WHERE id=$1`, content)
	reject(publish, uuid.New(), fresh, user, bank.BankID, content, lease)
}

func TestLearningReportMigrationRollbackPreservesPracticeContent(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-report-rollback")
	source, err := os.ReadFile("../db/migrations/000012_learning_reports.down.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, string(source)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_questions WHERE bank_id=$1`, bank.BankID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(bank.Questions) {
		t.Fatalf("rollback changed questions: %d", count)
	}
	applied, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations")
	if err != nil {
		t.Fatal(err)
	}
	if len(applied.Applied) != 1 || applied.Applied[0].Version != "000012" {
		t.Fatalf("reapply = %+v", applied)
	}
}
