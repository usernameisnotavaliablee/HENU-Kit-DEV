package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
)

// This test-only approval fixture is synthetic, never a real release receipt.
func installLearningTestContent(t *testing.T, pool *pgxpool.Pool, bank quizcraft.ImportReport) uuid.UUID {
	t.Helper()
	document := quizcraft.LearningContentDocument{SchemaVersion: 1, Tags: []quizcraft.LearningContentTag{
		{ID: "math", Kind: "knowledge", Name: "算术", Definition: "基础运算"},
		{ID: "trace", Kind: "ability", Name: "过程追踪", Definition: "追踪表达式求值"},
		{ID: "advanced", Kind: "knowledge", Name: "进阶概念", Definition: "进阶章节概念"},
	}}
	members := map[uuid.UUID]uuid.UUID{}
	for _, q := range bank.Questions {
		id, version := uuid.MustParse(q.QuestionID), uuid.MustParse(q.QuestionVersionID)
		members[id] = version
		tags := []string{"math", "trace"}
		if q.SourceQuestionID == "q0002" {
			tags = []string{"advanced"}
		}
		if q.SourceQuestionID == "q0001" || q.SourceQuestionID == "q0002" {
			document.Questions = append(document.Questions, quizcraft.LearningContentQuestion{QuestionID: id, QuestionVersionID: version, TagIDs: tags})
		}
	}
	raw, _ := json.Marshal(document)
	_, digest, err := quizcraft.ParseLearningContent(raw, members)
	if err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if _, err := pool.Exec(context.Background(), `INSERT INTO quizcraft_learning_content_versions(id,bank_id,bank_version_id,content_sha256,document,status,reviewed_by,reviewed_at) VALUES($1,$2,$3,$4,$5,'approved',$6,now())`, id, bank.BankID, bank.BankVersionID, digest, raw, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `INSERT INTO quizcraft_learning_catalogs(bank_id,active_content_version_id,enabled) VALUES($1,$2,true) ON CONFLICT(bank_id) DO UPDATE SET active_content_version_id=excluded.active_content_version_id,enabled=true`, bank.BankID, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLearningEvidenceUsesRealScopedAttemptsAndStableFingerprints(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-evidence")
	contentID := installLearningTestContent(t, pool, bank)
	user, outsider := uuid.New(), uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_learning_report_preferences(user_id,bank_id,enabled,external_analysis_consent,consent_version,next_due_at) VALUES($1,$2,true,true,'v1',now()+interval '7 days')`, user, bank.BankID); err != nil {
		t.Fatal(err)
	}
	service, err := quizcraft.New(quizcraft.Config{Database: pool, AllowTestBootstrapActivation: true})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := quizcraft.NewPracticeHTTP(quizcraft.PracticeHTTPConfig{Database: pool, AuthHMACSecret: []byte(practiceAuthSecret)})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()
	bankID := uuid.MustParse(bank.BankID)
	snapshot := func(cutoff time.Time) quizcraft.LearningEvidenceSnapshot {
		t.Helper()
		got, err := service.BuildLearningEvidence(ctx, user, bankID, cutoff)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	empty := snapshot(time.Now())
	if empty.ContentVersionID != contentID || len(empty.Statistics) != 3 || len(empty.Evidence) != 0 {
		t.Fatalf("empty snapshot = %+v", empty)
	}
	for _, stat := range empty.Statistics {
		if stat.UniqueQuestionCount != 0 {
			t.Fatal("uncovered tag invented history")
		}
	}
	submit := func(owner uuid.UUID, answer int) {
		t.Helper()
		headers := map[string]string{"Cookie": "quizcraft_session=" + practiceToken(t, owner.String()), "Idempotency-Key": uuid.NewString()}
		code, body := requestJSON(t, http.MethodPost, server.URL+"/api/v1/practice/sessions", headers, map[string]any{"bank_id": bank.BankID, "bank_version_id": bank.BankVersionID, "mode": "random", "question_count": 4})
		if code != http.StatusCreated {
			t.Fatalf("session = %d %s", code, body)
		}
		var session apiEnvelope[practiceSessionResponse]
		decodeJSON(t, body, &session)
		for _, q := range session.Data.Questions {
			if q.Type != "single" {
				continue
			}
			headers["Idempotency-Key"] = uuid.NewString()
			payload := map[string]any{"question_id": q.QuestionID, "question_version_id": q.QuestionVersionID, "answer": answer}
			for replay := 0; replay < 2; replay++ {
				code, body = requestJSON(t, http.MethodPost, server.URL+"/api/v1/practice/sessions/"+session.Data.SessionID+"/answers", headers, payload)
				if code != http.StatusOK {
					t.Fatalf("answer = %d %s", code, body)
				}
			}
			return
		}
		t.Fatal("single-choice fixture missing")
	}
	submit(user, 0)
	firstCutoff := time.Now()
	submit(outsider, 0)
	submit(user, 1)
	first, latest := snapshot(firstCutoff), snapshot(time.Now())
	var contentJSON []byte
	if err := pool.QueryRow(ctx, `SELECT document FROM quizcraft_learning_content_versions WHERE id=$1`, contentID).Scan(&contentJSON); err != nil {
		t.Fatal(err)
	}
	var reviewed quizcraft.LearningContentDocument
	if err := json.Unmarshal(contentJSON, &reviewed); err != nil {
		t.Fatal(err)
	}
	if _, err := quizcraft.BuildLearningModelInput(latest, reviewed); err != nil {
		t.Fatalf("real snapshot to model: %v", err)
	}
	decision := quizcraft.LearningModelDecision{PrimaryTagID: "math", Findings: []quizcraft.LearningModelFinding{{TagID: "math", Status: "supported", EvidenceIDs: []string{latest.Evidence[0].EvidenceId}, PossibleReason: "可能还需要用不同题目确认表现。"}}}
	resultJSON, _ := json.Marshal(decision)
	if _, err := quizcraft.ValidateLearningModelDecision(resultJSON, latest, reviewed); err == nil {
		t.Fatal("real repeated question was treated as strong independent evidence")
	}
	decision.Findings[0].Status = "tentative"
	resultJSON, _ = json.Marshal(decision)
	reportResult, err := quizcraft.ComposeLearningReport(latest, reviewed, resultJSON, uuid.New(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if reportResult.Status != "insufficient_evidence" || reportResult.NextStep.Kind != "diagnostic" {
		t.Fatalf("real report overclaimed: %+v", reportResult)
	}
	schema, err := openapi3.NewLoader().LoadFromFile("../../../../packages/api-contracts/openapi/quizcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(reportResult)
	var response any
	if err := json.Unmarshal(encoded, &response); err != nil {
		t.Fatal(err)
	}
	if err := schema.Components.Schemas["LearningReport"].Value.VisitJSON(response); err != nil {
		t.Fatalf("composed report violates published contract: %v", err)
	}

	for _, stat := range first.Statistics {
		if stat.TagId == "math" && (stat.AttemptCount != 1 || stat.FirstCorrectCount != 0 || stat.RepeatAttemptCount != 0) {
			t.Fatalf("historical cutoff = %+v", stat)
		}
	}
	for _, stat := range latest.Statistics {
		if stat.TagId == "advanced" {
			if stat.AttemptCount != 0 {
				t.Fatal("unanswered chapter acquired evidence")
			}
			continue
		}
		if stat.AttemptCount != 2 || stat.UniqueQuestionCount != 1 || stat.FirstCorrectCount != 0 || stat.RepeatAttemptCount != 1 || stat.RepeatCorrectCount != 1 || stat.LatestCorrectCount != 1 {
			t.Fatalf("replay/owner/repeat isolation = %+v", stat)
		}
	}
	if len(latest.Evidence) != 2 || latest.InputSHA256 == first.InputSHA256 || latest.InputSHA256 != snapshot(time.Now()).InputSHA256 {
		t.Fatal("evidence or deterministic fingerprint incorrect")
	}
	// Simulate an anomalous legacy row; the standard importer rejects this shape.
	var single quizcraft.ImportedQuestion
	for _, q := range bank.Questions {
		if q.SourceQuestionID == "q0001" {
			single = q
		}
	}
	legacySession := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_sessions(id,bank_id,bank_version_id,user_id,actor_key,mode) VALUES($1,$2,$3,$4,'user:'||$4::uuid::text,'random')`, legacySession, bank.BankID, bank.BankVersionID, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_session_questions(session_id,bank_id,bank_version_id,question_id,question_version_id,position) VALUES($1,$2,$3,$4,$5,1)`, legacySession, bank.BankID, bank.BankVersionID, single.QuestionID, single.QuestionVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_attempts(id,session_id,bank_id,bank_version_id,question_id,question_version_id,user_id,submitted_answer,correct,expected_answer,response_body) VALUES($1,$2,$3,$4,$5,$6,$7,'0',false,'null','{}')`, uuid.New(), legacySession, bank.BankID, bank.BankVersionID, single.QuestionID, single.QuestionVersionID, user); err != nil {
		t.Fatal(err)
	}
	if snapshot(time.Now()).InputSHA256 != latest.InputSHA256 {
		t.Fatal("unscorable legacy attempt changed assessed evidence")
	}
	for _, e := range latest.Evidence {
		if e.SubmittedAt.After(latest.Cutoff) || e.QuestionId != latest.Evidence[0].QuestionId {
			t.Fatal("foreign or future sample")
		}
	}
	if _, err := service.BuildLearningEvidence(ctx, uuid.Nil, bankID, time.Now()); err == nil {
		t.Fatal("guest accepted")
	}
	if _, err := service.BuildLearningEvidence(ctx, outsider, bankID, time.Now()); err == nil {
		t.Fatal("missing consent accepted")
	}
	otherBank := importPracticeBank(t, pool, "learning-evidence-other")
	if _, err := service.BuildLearningEvidence(ctx, user, uuid.MustParse(otherBank.BankID), time.Now()); err == nil {
		t.Fatal("cross-bank content accepted")
	}

	// A bank revision must retain history for unchanged question versions.
	revised, err := service.ImportJSON(ctx, "learning-evidence", []byte(strings.Replace(validBank, "选择偶数", "选择所有偶数", 1)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BuildLearningEvidence(ctx, user, bankID, time.Now()); err == nil {
		t.Fatal("stale reviewed content accepted")
	}
	installLearningTestContent(t, pool, revised)
	kept := snapshot(time.Now())
	for _, stat := range kept.Statistics {
		if stat.TagId == "math" && stat.AttemptCount != 2 {
			t.Fatal("unchanged question lost real history")
		}
	}
	revised, err = service.ImportJSON(ctx, "learning-evidence", []byte(strings.Replace(validBank, "1+1=?", "1 + 1 = ?", 1)))
	if err != nil {
		t.Fatal(err)
	}
	installLearningTestContent(t, pool, revised)
	for _, stat := range snapshot(time.Now()).Statistics {
		if stat.AttemptCount != 0 {
			t.Fatal("changed question inherited old evidence")
		}
	}

	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET chapter_ids='["ch02"]',revision=revision+1 WHERE user_id=$1 AND bank_id=$2`, user, bankID); err != nil {
		t.Fatal(err)
	}
	scoped := snapshot(time.Now())
	if len(scoped.Statistics) != 1 || scoped.Statistics[0].TagId != "advanced" {
		t.Fatalf("chapter scope = %+v", scoped.Statistics)
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET chapter_ids='["unknown"]' WHERE user_id=$1 AND bank_id=$2`, user, bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BuildLearningEvidence(ctx, user, bankID, time.Now()); err == nil {
		t.Fatal("unknown chapter broadened scope")
	}
	if _, err := pool.Exec(ctx, `UPDATE quizcraft_learning_report_preferences SET chapter_ids='[]',enabled=false WHERE user_id=$1 AND bank_id=$2`, user, bankID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.BuildLearningEvidence(ctx, user, bankID, time.Now()); err == nil {
		t.Fatal("revoked consent accepted")
	}
}

func TestLearningEvidenceNeverFetchesOversizedHistoricAnswers(t *testing.T) {
	ctx := context.Background()
	pool := isolatedArtifactDatabase(t)
	if _, err := quizcraft.ApplyVersionedMigrations(ctx, pool, "../db/migrations"); err != nil {
		t.Fatal(err)
	}
	bank := importPracticeBank(t, pool, "learning-answer-bound")
	installLearningTestContent(t, pool, bank)
	user := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_learning_report_preferences(user_id,bank_id,enabled,external_analysis_consent,consent_version,next_due_at) VALUES($1,$2,true,true,'v1',now()+interval '7 days')`, user, bank.BankID); err != nil {
		t.Fatal(err)
	}
	var single quizcraft.ImportedQuestion
	for _, q := range bank.Questions {
		if q.SourceQuestionID == "q0001" {
			single = q
		}
	}
	if single.QuestionID == "" {
		t.Fatal("missing test question")
	}
	session := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_sessions(id,bank_id,bank_version_id,user_id,actor_key,mode) VALUES($1,$2,$3,$4,'user:'||$4::uuid::text,'random')`, session, bank.BankID, bank.BankVersionID, user); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_session_questions(session_id,bank_id,bank_version_id,question_id,question_version_id,position) VALUES($1,$2,$3,$4,$5,1)`, session, bank.BankID, bank.BankVersionID, single.QuestionID, single.QuestionVersionID); err != nil {
		t.Fatal(err)
	}
	answer, _ := json.Marshal(strings.Repeat("private", 1<<18))
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_attempts(id,session_id,bank_id,bank_version_id,question_id,question_version_id,user_id,submitted_answer,correct,expected_answer,response_body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,false,'1','{}')`, uuid.New(), session, bank.BankID, bank.BankVersionID, single.QuestionID, single.QuestionVersionID, user, answer); err != nil {
		t.Fatal(err)
	}
	service, err := quizcraft.New(quizcraft.Config{Database: pool, AllowTestBootstrapActivation: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.BuildLearningEvidence(ctx, user, uuid.MustParse(bank.BankID), time.Now()); err == nil {
		t.Fatal("oversized saved answer must fail closed, not return or truncate private content")
	}
}
