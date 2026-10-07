package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"henukit.dev/quizcraft/internal/contract"
)

// practiceSessionEnvelopeData mirrors the pinned-session response; the contract
// renders questions as a union, so only the identity fields are read here.
type practiceSessionEnvelopeData struct {
	SessionId                uuid.UUID `json:"session_id"`
	BankId                   uuid.UUID `json:"bank_id"`
	Mode                     string    `json:"mode"`
	ExcludedUnavailableCount int       `json:"excluded_unavailable_count"`
	Questions                []struct {
		QuestionId uuid.UUID `json:"question_id"`
	} `json:"questions"`
}

func learningPracticeSessionPath(bankID, reportID string) string {
	return "/api/v1/portal/practice/banks/" + bankID + "/learning-reports/results/" + reportID + "/practice-sessions"
}

// seedOneLearningAttempt gives the owner exactly one answered question, so a
// published report can be built on real evidence instead of a cold start.
func seedOneLearningAttempt(t *testing.T, pool *pgxpool.Pool, bankID, owner uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	var bankVersion, questionID, questionVersion uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT b.active_version_id,m.question_id,m.question_version_id
        FROM quizcraft_banks b
        JOIN quizcraft_bank_version_questions m ON m.bank_id=b.id AND m.bank_version_id=b.active_version_id
        JOIN quizcraft_question_versions q ON q.bank_id=b.id AND q.id=m.question_version_id
        WHERE b.id=$1 AND q.type='single' ORDER BY m.question_id LIMIT 1`, bankID).Scan(&bankVersion, &questionID, &questionVersion); err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_sessions(id,bank_id,bank_version_id,user_id,actor_key,mode) VALUES($1,$2,$3,$4,'user:'||$4::uuid::text,'random')`, session, bankID, bankVersion, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_session_questions(session_id,bank_id,bank_version_id,question_id,question_version_id,position) VALUES($1,$2,$3,$4,$5,1)`, session, bankID, bankVersion, questionID, questionVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO quizcraft_practice_attempts(id,session_id,bank_id,bank_version_id,question_id,question_version_id,user_id,submitted_answer,correct,expected_answer,response_body) VALUES($1,$2,$3,$4,$5,$6,$7,'0',false,'1','{}')`, uuid.New(), session, bankID, bankVersion, questionID, questionVersion, owner); err != nil {
		t.Fatal(err)
	}
}

func TestLearningReportPracticeSessionPinsServerSelectedQuestions(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-practice")
	publishLearningTestReport(t, service, owner, bankID, versions)
	report, err := service.GetLatestLearningReport(ctx, owner, bankID)
	if err != nil {
		t.Fatal(err)
	}
	if (report.NextStep.Kind != contract.Practice && report.NextStep.Kind != contract.Diagnostic) || report.NextStep.QuestionIds == nil || len(*report.NextStep.QuestionIds) == 0 {
		t.Fatalf("test premise lost: report recommendation = %+v", report.NextStep)
	}
	recommended := map[uuid.UUID]bool{}
	for _, id := range *report.NextStep.QuestionIds {
		recommended[id] = true
	}

	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningWriteHandler(t, pool, allowedClient, versions, false)
	path := learningPracticeSessionPath(bankID.String(), report.ReportId.String())

	first := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-practice-pin-0001")
	status, body, _ := sendPortalPracticeCommand(t, first)
	if status != http.StatusCreated {
		t.Fatalf("report practice session = %d %s", status, body)
	}
	envelope := map[string]any{}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	schema, err := openapi3.NewLoader().LoadFromFile("../../../../packages/api-contracts/openapi/quizcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := schema.Components.Schemas["PracticeSessionEnvelope"].Value.VisitJSON(envelope); err != nil {
		t.Fatalf("practice session envelope violates contract: %v", err)
	}
	var session practiceSessionEnvelopeData
	decodeLearningEnvelope(t, body, &session)
	if session.Mode != "report" || session.BankId != bankID || session.ExcludedUnavailableCount != 0 {
		t.Fatalf("pinned session = %+v", session)
	}
	if len(session.Questions) != len(recommended) {
		t.Fatalf("served %d questions, report recommended %d", len(session.Questions), len(recommended))
	}
	for _, question := range session.Questions {
		if !recommended[question.QuestionId] {
			t.Fatalf("session served a question the report did not recommend: %s", question.QuestionId)
		}
	}
	var sessions, pinned int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_practice_sessions WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_practice_session_questions WHERE session_id=$1`, session.SessionId).Scan(&pinned); err != nil || pinned != len(session.Questions) {
		t.Fatalf("pinned questions = %d %v", pinned, err)
	}

	replay := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-practice-pin-0001")
	replayStatus, replayBody, _ := sendPortalPracticeCommand(t, replay)
	if replayStatus != http.StatusCreated || !bytes.Equal(body, replayBody) {
		t.Fatalf("replay must return the pinned session: %d %s", replayStatus, replayBody)
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_practice_sessions WHERE user_id=$1 AND bank_id=$2`, owner, bankID).Scan(&after); err != nil || after != sessions {
		t.Fatalf("replay created another session: %d %v", after, err)
	}

	// A different key pins its own session: sessions are per request, not per report.
	second := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-practice-pin-0002")
	secondStatus, secondBody, _ := sendPortalPracticeCommand(t, second)
	if secondStatus != http.StatusCreated {
		t.Fatalf("second pinned session = %d %s", secondStatus, secondBody)
	}
	var secondSession practiceSessionEnvelopeData
	decodeLearningEnvelope(t, secondBody, &secondSession)
	if secondSession.SessionId == session.SessionId {
		t.Fatal("a new idempotency key must not reuse the previous session")
	}

	other := uuid.NewSHA1(uuid.NameSpaceOID, []byte("learning-practice-other:"+bankID.String()))
	crossOwner := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, other.String(), "learning-practice-cross-0001")
	if status, respBody, _ := sendPortalPracticeCommand(t, crossOwner); status != http.StatusNotFound {
		t.Fatalf("another owner pinned a report session: %d %s", status, respBody)
	}
	malformed := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, learningPracticeSessionPath(bankID.String(), "not-a-uuid"), nil, owner.String(), "learning-practice-bad-id-0001")
	if status, respBody, _ := sendPortalPracticeCommand(t, malformed); status != http.StatusBadRequest {
		t.Fatalf("malformed report id = %d %s", status, respBody)
	}
	revokedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return false, http.StatusOK })
	revoked := newLearningWriteHandler(t, pool, revokedClient, versions, false)
	revokedRequest := newPortalPracticeCommandRequest(t, http.MethodPost, revoked.URL, path, nil, owner.String(), "learning-practice-revoked-0001")
	if status, respBody, _ := sendPortalPracticeCommand(t, revokedRequest); status != http.StatusForbidden {
		t.Fatalf("revoked owner pinned a session: %d %s", status, respBody)
	}

	// Superseded reports cannot be practised from.
	if _, err := service.UpdateLearningReportPreferences(ctx, owner, bankID, contract.LearningReportPreferencesUpdate{Enabled: false, IntervalDays: 7, Goal: "follow_course", ChapterIds: []string{}}); err != nil {
		t.Fatal(err)
	}
	stale := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-practice-stale-0001")
	if status, respBody, _ := sendPortalPracticeCommand(t, stale); status != http.StatusNotFound {
		t.Fatalf("stale report produced a session: %d %s", status, respBody)
	}
}

func TestLearningReportPracticeSessionRefusesReportsWithoutRecommendation(t *testing.T) {
	ctx := context.Background()
	pool, service, owner, bankID, versions := newLearningLeaseTest(t, "learning-practice-none")
	seedOneLearningAttempt(t, pool, bankID, owner)
	if _, _, err := service.QueueLearningReport(ctx, owner, bankID, time.Now(), "manual", versions); err != nil {
		t.Fatal(err)
	}
	lease, ok, err := service.ClaimNextLearningReport(ctx, time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %t %v", ok, err)
	}
	// No finding and too little coverage: the server keeps the "no action" next
	// step, so there is nothing to practise and no question may be invented.
	decision := []byte(`{"findings":[],"primary_tag_id":""}`)
	report, err := service.PublishLearningReport(ctx, lease, decision, versions, func(_ context.Context, id uuid.UUID) (bool, error) { return id == owner, nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.NextStep.Kind != contract.NoAction || report.NextStep.QuestionIds != nil {
		t.Fatalf("test premise lost: next step = %+v", report.NextStep)
	}
	allowedClient, _ := learningRunnerClient(t, func(int32) (bool, int) { return true, http.StatusOK })
	server := newLearningWriteHandler(t, pool, allowedClient, versions, false)
	path := learningPracticeSessionPath(bankID.String(), report.ReportId.String())
	request := newPortalPracticeCommandRequest(t, http.MethodPost, server.URL, path, nil, owner.String(), "learning-practice-none-0001")
	status, body, _ := sendPortalPracticeCommand(t, request)
	if status != http.StatusConflict || !bytes.Contains(body, []byte(`"code":"learning_no_practice"`)) {
		t.Fatalf("no-recommendation report = %d %s", status, body)
	}
	var sessions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_practice_sessions WHERE user_id=$1 AND mode='report'`, owner).Scan(&sessions); err != nil || sessions != 0 {
		t.Fatalf("no session may be pinned: %d %v", sessions, err)
	}
}
