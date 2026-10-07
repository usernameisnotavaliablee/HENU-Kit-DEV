package tests

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
	"henukit.dev/quizcraft/internal/contract"
)

// TestLearningReportMemberJourneyRunsTheWholeMemberPath is the integration
// counterpart of the feature-scoped tests: it drives the real HTTP write
// boundary, the real background worker, the real HTTP read boundary and the
// withdrawal route against one PostgreSQL database. Those pieces were each
// covered with the other side faked, so this test is what proves they compose:
// a queued request is invisible to reads until the worker publishes it, the
// published report is readable by its owner, a report without a practice
// recommendation cannot invent a session, and withdrawal removes the derived
// data while preserving the owner's original attempts.
func TestLearningReportMemberJourneyRunsTheWholeMemberPath(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-journey")
	// The stock test content tags only two questions, which can never reach the
	// three-question coverage a ready report needs, so this journey installs its
	// own approved content version over the same bank.
	installJourneyLearningContent(t, pool, bankID)
	const journeyAttempts = 3
	seedJourneyAttempts(t, pool, bankID, owner, journeyAttempts)

	var attemptsBefore int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_practice_attempts WHERE user_id=$1`, owner).Scan(&attemptsBefore); err != nil || attemptsBefore != journeyAttempts {
		t.Fatalf("journey premise lost: attempts=%d %v", attemptsBefore, err)
	}

	entitled, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	writes := newLearningWriteHandler(t, pool, entitled, versions, false)
	reads := newLearningReadsHandler(t, pool, entitled, true)
	bank := bankID.String()
	collection := learningReportPath("", bank)
	latestPath := "/api/v1/portal/practice/banks/" + bank + "/learning-reports/latest"

	// Premise: the installed content and the owner's attempts must produce a
	// provider-ready input, otherwise later steps would fail for fixture reasons.
	snapshot, err := service.BuildLearningEvidence(ctx, owner, bankID, time.Now())
	if err != nil {
		t.Fatalf("journey evidence: %v", err)
	}
	var documentRaw []byte
	if err := pool.QueryRow(ctx, `SELECT document FROM quizcraft_learning_content_versions WHERE bank_id=$1 AND id=$2`, bankID, snapshot.ContentVersionID).Scan(&documentRaw); err != nil {
		t.Fatalf("journey content: %v", err)
	}
	var journeyDocument quizcraft.LearningContentDocument
	if err := json.Unmarshal(documentRaw, &journeyDocument); err != nil {
		t.Fatal(err)
	}
	if _, err := quizcraft.BuildLearningModelInput(snapshot, journeyDocument); err != nil {
		t.Fatalf("journey model input: %v", err)
	}

	// 1. The owner enables generation through the write boundary.
	enable := newPortalPracticeCommandRequest(t, http.MethodPut, writes.URL, collection+"/preferences", learningPreferencesBody(true, true), owner.String(), "learning-journey-preferences-01")
	if status, body, _ := sendPortalPracticeCommand(t, enable); status != http.StatusOK {
		t.Fatalf("enable preferences = %d: %s", status, body)
	}

	// 2. The owner asks for a report: one queued task, no report yet.
	request := newPortalPracticeCommandRequest(t, http.MethodPost, writes.URL, collection, nil, owner.String(), "learning-journey-request-01")
	status, body, _ := sendPortalPracticeCommand(t, request)
	if status != http.StatusAccepted {
		t.Fatalf("queue report = %d: %s", status, body)
	}
	var queued struct {
		TaskId uuid.UUID `json:"task_id"`
		Status string    `json:"status"`
	}
	decodeLearningEnvelope(t, body, &queued)
	if queued.TaskId == uuid.Nil || queued.Status != "queued" {
		t.Fatalf("queued task = %+v", queued)
	}

	// 3. Before any worker runs, reads must not invent a report.
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, reads.URL+latestPath, owner.String())); status != http.StatusNotFound {
		t.Fatalf("unpublished latest report = %d, want 404", status)
	}
	taskPath := "/api/v1/portal/practice/banks/" + bank + "/learning-reports/tasks/" + queued.TaskId.String()
	taskStatus, taskBody := sendCatalogRequest(t, newPortalCatalogGET(t, reads.URL+taskPath, owner.String()))
	if taskStatus != http.StatusOK {
		t.Fatalf("queued task read = %d: %s", taskStatus, taskBody)
	}
	var pending struct {
		Status   string     `json:"status"`
		ReportId *uuid.UUID `json:"report_id"`
	}
	decodeLearningEnvelope(t, taskBody, &pending)
	if pending.Status != "queued" || pending.ReportId != nil {
		t.Fatalf("pending task = %+v", pending)
	}

	// 4. The worker claims the task and publishes from the owner's real evidence.
	modelCalls := 0
	provider := func(_ context.Context, input quizcraft.LearningModelInput, current quizcraft.LearningJobVersions) ([]byte, error) {
		modelCalls++
		if current != versions || len(input.Evidence) != journeyAttempts {
			t.Errorf("journey reached the provider with an unscoped payload: %+v", input)
		}
		for _, evidence := range input.Evidence {
			if evidence.EvidenceID == "" || len(evidence.TagIDs) == 0 {
				t.Errorf("provider evidence is unscoped: %+v", evidence)
			}
			if evidence.AnswerWithheld && evidence.SubmittedAnswer != nil {
				t.Errorf("withheld answer leaked to the provider: %+v", evidence)
			}
		}
		return []byte(`{"findings":[],"primary_tag_id":""}`), nil
	}
	processed, err := service.ProcessNextLearningReport(ctx, entitled, provider, versions, time.Minute)
	if !processed || err != nil || modelCalls != 1 {
		var status, reason string
		_ = pool.QueryRow(ctx, `SELECT status,reason_code FROM quizcraft_learning_report_jobs WHERE id=$1`, queued.TaskId).Scan(&status, &reason)
		t.Fatalf("worker publish: processed=%t err=%v model=%d job=%s/%s", processed, err, modelCalls, status, reason)
	}

	var reportID uuid.UUID
	var reportStatus string
	if err := pool.QueryRow(ctx, `SELECT id,status FROM quizcraft_learning_reports WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&reportID, &reportStatus); err != nil || reportStatus != "ready" {
		t.Fatalf("published report row = %s %v", reportStatus, err)
	}

	// 5. The published report and its task are now readable by the owner only.
	latestStatus, latestBody := sendCatalogRequest(t, newPortalCatalogGET(t, reads.URL+latestPath, owner.String()))
	if latestStatus != http.StatusOK {
		t.Fatalf("published latest report = %d: %s", latestStatus, latestBody)
	}
	var published struct {
		ReportId uuid.UUID `json:"report_id"`
		Status   string    `json:"status"`
		NextStep struct {
			Kind contract.LearningReportActionKind `json:"kind"`
		} `json:"next_step"`
	}
	decodeLearningEnvelope(t, latestBody, &published)
	if published.ReportId != reportID || published.Status != "ready" || published.NextStep.Kind != contract.NoAction {
		t.Fatalf("published report = %+v", published)
	}
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, reads.URL+latestPath, uuid.NewString())); status != http.StatusNotFound {
		t.Fatalf("another owner read the report with status %d, want 404", status)
	}
	taskStatus, taskBody = sendCatalogRequest(t, newPortalCatalogGET(t, reads.URL+taskPath, owner.String()))
	if taskStatus != http.StatusOK {
		t.Fatalf("ready task read = %d: %s", taskStatus, taskBody)
	}
	var ready struct {
		Status   string     `json:"status"`
		ReportId *uuid.UUID `json:"report_id"`
	}
	decodeLearningEnvelope(t, taskBody, &ready)
	if ready.Status != "ready" || ready.ReportId == nil || *ready.ReportId != reportID {
		t.Fatalf("ready task = %+v", ready)
	}

	// 6. A report with no practice recommendation cannot fabricate a session.
	pin := newPortalPracticeCommandRequest(t, http.MethodPost, writes.URL, learningPracticeSessionPath(bank, reportID.String()), nil, owner.String(), "learning-journey-practice-01")
	if status, body, _ := sendPortalPracticeCommand(t, pin); status != http.StatusConflict {
		t.Fatalf("practice from a no-action report = %d: %s", status, body)
	}
	var sessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_practice_sessions WHERE user_id=$1`, owner).Scan(&sessions); err != nil || sessions != 1 {
		t.Fatalf("no-action report created a session: count=%d %v", sessions, err)
	}

	// 7. Withdrawal clears the derived data and keeps the original attempts.
	clear := newPortalPracticeCommandRequest(t, http.MethodDelete, writes.URL, collection, nil, owner.String(), "learning-journey-clear-01")
	clearStatus, clearBody, _ := sendPortalPracticeCommand(t, clear)
	if clearStatus != http.StatusOK {
		t.Fatalf("clear reports = %d: %s", clearStatus, clearBody)
	}
	var cleared struct {
		Cleared  bool  `json:"cleared"`
		Revision int64 `json:"revision"`
	}
	decodeLearningEnvelope(t, clearBody, &cleared)
	if !cleared.Cleared || cleared.Revision < 1 {
		t.Fatalf("clear result = %+v", cleared)
	}
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, reads.URL+latestPath, owner.String())); status != http.StatusNotFound {
		t.Fatalf("cleared latest report = %d, want 404", status)
	}
	if status, _ := sendCatalogRequest(t, newPortalCatalogGET(t, reads.URL+taskPath, owner.String())); status != http.StatusNotFound {
		t.Fatalf("cleared task read = %d, want 404", status)
	}
	var reports, jobs, attemptsAfter int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_reports WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&reports); err != nil || reports != 0 {
		t.Fatalf("reports after clear = %d %v", reports, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_report_jobs WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("jobs after clear = %d %v", jobs, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_practice_attempts WHERE user_id=$1`, owner).Scan(&attemptsAfter); err != nil || attemptsAfter != attemptsBefore {
		t.Fatalf("clear removed original attempts: before=%d after=%d %v", attemptsBefore, attemptsAfter, err)
	}
	var consent bool
	if err := pool.QueryRow(ctx, `SELECT external_analysis_consent FROM quizcraft_learning_report_preferences WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&consent); err != nil || consent {
		t.Fatalf("clear left analysis consent on: %t %v", consent, err)
	}
	// The bank itself is untouched: withdrawal is owner-scoped, not a content edit.
	var banks int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_banks WHERE id=$1`, bankID).Scan(&banks); err != nil || banks != 1 {
		t.Fatalf("clear touched the bank: %d %v", banks, err)
	}

	// 8. Withdrawal is not a soft delete: the owner must grant fresh consent
	// before another report is queued, and the cleared task is never reused.
	refused := newPortalPracticeCommandRequest(t, http.MethodPost, writes.URL, collection, nil, owner.String(), "learning-journey-request-02")
	if status, body, _ := sendPortalPracticeCommand(t, refused); status != http.StatusConflict {
		t.Fatalf("request without fresh consent = %d: %s", status, body)
	}
	renew := newPortalPracticeCommandRequest(t, http.MethodPut, writes.URL, collection+"/preferences", learningPreferencesBody(true, true), owner.String(), "learning-journey-preferences-02")
	if status, body, _ := sendPortalPracticeCommand(t, renew); status != http.StatusOK {
		t.Fatalf("renew preferences = %d: %s", status, body)
	}
	again := newPortalPracticeCommandRequest(t, http.MethodPost, writes.URL, collection, nil, owner.String(), "learning-journey-request-03")
	againStatus, againBody, _ := sendPortalPracticeCommand(t, again)
	if againStatus != http.StatusAccepted {
		t.Fatalf("re-request after clear = %d: %s", againStatus, againBody)
	}
	var requisitioned struct {
		TaskId uuid.UUID `json:"task_id"`
		Status string    `json:"status"`
	}
	decodeLearningEnvelope(t, againBody, &requisitioned)
	if requisitioned.TaskId == queued.TaskId || requisitioned.Status != "queued" {
		t.Fatalf("re-request reused the cleared task: %+v (first %s)", requisitioned, queued.TaskId)
	}
}

// journeyQuestion is one bank question with the import source id that the
// learning content document keys its tags by.
type journeyQuestion struct {
	sourceID string
	question uuid.UUID
	version  uuid.UUID
	answer   []byte
}

func journeyBankQuestions(t *testing.T, pool *pgxpool.Pool, bankID uuid.UUID) []journeyQuestion {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT s.source_question_id,m.question_id,m.question_version_id,q.answer
        FROM quizcraft_banks b
        JOIN quizcraft_bank_version_questions m ON m.bank_id=b.id AND m.bank_version_id=b.active_version_id
        JOIN quizcraft_questions s ON s.bank_id=b.id AND s.id=m.question_id
        JOIN quizcraft_question_versions q ON q.bank_id=b.id AND q.id=m.question_version_id
        WHERE b.id=$1 ORDER BY s.source_question_id`, bankID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var questions []journeyQuestion
	for rows.Next() {
		var question journeyQuestion
		if err := rows.Scan(&question.sourceID, &question.question, &question.version, &question.answer); err != nil {
			t.Fatal(err)
		}
		questions = append(questions, question)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(questions) < 4 {
		t.Fatalf("journey requires a bank with at least four questions, got %d", len(questions))
	}
	return questions
}

// installJourneyLearningContent approves a content version that tags three
// questions with one knowledge tag, which is the minimum coverage for a report
// that is allowed to claim the evidence is sufficient.
func installJourneyLearningContent(t *testing.T, pool *pgxpool.Pool, bankID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	questions := journeyBankQuestions(t, pool, bankID)
	document := quizcraft.LearningContentDocument{SchemaVersion: 1, Tags: []quizcraft.LearningContentTag{
		{ID: "math", Kind: "knowledge", Name: "算术", Definition: "基础运算"},
		{ID: "trace", Kind: "ability", Name: "过程追踪", Definition: "追踪表达式求值"},
		{ID: "advanced", Kind: "knowledge", Name: "进阶概念", Definition: "进阶章节概念"},
	}}
	members := map[uuid.UUID]uuid.UUID{}
	for _, question := range questions {
		members[question.question] = question.version
		if question.sourceID == questions[0].sourceID {
			document.Questions = append(document.Questions, quizcraft.LearningContentQuestion{QuestionID: question.question, QuestionVersionID: question.version, TagIDs: []string{"advanced"}})
			continue
		}
		document.Questions = append(document.Questions, quizcraft.LearningContentQuestion{QuestionID: question.question, QuestionVersionID: question.version, TagIDs: []string{"math", "trace"}})
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, err := quizcraft.ParseLearningContent(raw, members)
	if err != nil {
		t.Fatal(err)
	}
	var bankVersion uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT active_version_id FROM quizcraft_banks WHERE id=$1`, bankID).Scan(&bankVersion); err != nil {
		t.Fatal(err)
	}
	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_learning_content_versions(id,bank_id,bank_version_id,content_sha256,document,status,reviewed_by,reviewed_at) VALUES($1,$2,$3,$4,$5,'approved',$6,now())`, id, bankID, bankVersion, digest, raw, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_learning_catalogs(bank_id,active_content_version_id,enabled) VALUES($1,$2,true) ON CONFLICT(bank_id) DO UPDATE SET active_content_version_id=excluded.active_content_version_id,enabled=true`, bankID, id); err != nil {
		t.Fatal(err)
	}
}

// seedJourneyAttempts answers `count` distinct tagged questions, which is what
// turns the tag statistics into real coverage.
func seedJourneyAttempts(t *testing.T, pool *pgxpool.Pool, bankID, owner uuid.UUID, count int) {
	t.Helper()
	ctx := context.Background()
	questions := journeyBankQuestions(t, pool, bankID)
	var bankVersion uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT active_version_id FROM quizcraft_banks WHERE id=$1`, bankID).Scan(&bankVersion); err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_sessions(id,bank_id,bank_version_id,user_id,actor_key,mode) VALUES($1,$2,$3,$4,'user:'||$4::uuid::text,'random')`, session, bankID, bankVersion, owner); err != nil {
		t.Fatal(err)
	}
	position := 0
	for _, question := range questions {
		if question.sourceID == questions[0].sourceID {
			continue
		}
		position++
		if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_session_questions(session_id,bank_id,bank_version_id,question_id,question_version_id,position) VALUES($1,$2,$3,$4,$5,$6)`, session, bankID, bankVersion, question.question, question.version, position); err != nil {
			t.Fatal(err)
		}
		// The authoritative answer is reused for both columns: the journey needs
		// real, self-consistent answer facts, and the provider payload validates
		// the expected answer against the reviewed question.
		if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_attempts(id,session_id,bank_id,bank_version_id,question_id,question_version_id,user_id,submitted_answer,correct,expected_answer,response_body) VALUES($1,$2,$3,$4,$5,$6,$7,$8,true,$8,'{}')`, uuid.New(), session, bankID, bankVersion, question.question, question.version, owner, question.answer); err != nil {
			t.Fatal(err)
		}
		if position == count {
			break
		}
	}
	if position != count {
		t.Fatalf("seeded %d of %d journey attempts", position, count)
	}
}
