package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
)

// The Workshop learning-content review surface is the only path from a content
// package to a member-facing report: import stores a draft, approval names the
// reviewing operator, and activation decides which version members actually get.

type learningReviewFixture struct {
	pool    *pgxpool.Pool
	service *quizcraft.Service
	owner   uuid.UUID
	bankID  uuid.UUID
	actorID uuid.UUID
	url     string
	auth    map[string]string
}

type learningReviewVersion struct {
	ContentVersionID uuid.UUID  `json:"content_version_id"`
	BankVersionID    uuid.UUID  `json:"bank_version_id"`
	ContentSHA256    string     `json:"content_sha256"`
	Status           string     `json:"status"`
	ReviewedBy       *uuid.UUID `json:"reviewed_by"`
	ReviewedAt       *time.Time `json:"reviewed_at"`
	Active           bool       `json:"active"`
	CatalogEnabled   bool       `json:"catalog_enabled"`
	QuestionCount    int        `json:"question_count"`
	LessonCount      int        `json:"lesson_count"`
}

func newLearningReviewTest(t *testing.T, name string) learningReviewFixture {
	t.Helper()
	pool, service, owner, bankID, _ := newLearningLeaseTest(t, name)
	handler, err := quizcraft.NewPracticeHTTP(quizcraft.PracticeHTTPConfig{Database: pool, AuthHMACSecret: []byte(practiceAuthSecret), AllowTestWorkshopClaims: true})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	actorID := uuid.New()
	bankScope := []map[string]string{{"kind": "resource", "product_code": "quizcraft", "resource_type": "bank", "resource_id": bankID.String()}}
	return learningReviewFixture{
		pool:    pool,
		service: service,
		owner:   owner,
		bankID:  bankID,
		actorID: actorID,
		url:     server.URL,
		auth: map[string]string{
			"read":    "quizcraft_session=" + workshopToken(t, actorID.String(), []string{"quizcraft.workshop.read"}, bankScope),
			"write":   "quizcraft_session=" + workshopToken(t, actorID.String(), []string{"quizcraft.workshop.write"}, bankScope),
			"publish": "quizcraft_session=" + workshopToken(t, actorID.String(), []string{"quizcraft.workshop.publish"}, bankScope),
		},
	}
}

func (fixture learningReviewFixture) collection() string {
	return fmt.Sprintf("%s/api/v1/workshop/banks/%s/learning-content", fixture.url, fixture.bankID)
}

func (fixture learningReviewFixture) headers(scope, idempotencyKey string) map[string]string {
	headers := map[string]string{"Cookie": fixture.auth[scope]}
	if idempotencyKey != "" {
		headers["Idempotency-Key"] = idempotencyKey
	}
	return headers
}

// learningReviewPackage builds a candidate package over real published question
// versions. `tamper` rewrites one version id, which the import validation must
// reject because membership comes from the database, never from the payload.
func learningReviewPackage(t *testing.T, pool *pgxpool.Pool, bankID uuid.UUID, tagID string, questionCount int, tamper bool) json.RawMessage {
	t.Helper()
	questions := journeyBankQuestions(t, pool, bankID)
	if len(questions) < questionCount {
		t.Fatalf("bank has %d questions, need %d", len(questions), questionCount)
	}
	document := quizcraft.LearningContentDocument{SchemaVersion: 1, Tags: []quizcraft.LearningContentTag{{ID: tagID, Kind: "knowledge", Name: "复习点 " + tagID, Definition: "课程复习点定义"}}}
	for index, question := range questions {
		if index == questionCount {
			break
		}
		version := question.version
		if tamper && index == 0 {
			version = uuid.New()
		}
		document.Questions = append(document.Questions, quizcraft.LearningContentQuestion{QuestionID: question.question, QuestionVersionID: version, TagIDs: []string{tagID}})
	}
	raw, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := quizcraft.ParseLearningContent(raw, nil); err == nil {
		t.Fatal("fixture package must not validate against an empty membership map")
	}
	return raw
}

func decodeLearningReviewVersions(t *testing.T, body []byte) []learningReviewVersion {
	t.Helper()
	var envelope struct {
		Data []learningReviewVersion `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode learning content versions: %v: %s", err, body)
	}
	return envelope.Data
}

func learningReviewVersionByID(t *testing.T, versions []learningReviewVersion, contentID uuid.UUID) learningReviewVersion {
	t.Helper()
	for _, version := range versions {
		if version.ContentVersionID == contentID {
			return version
		}
	}
	t.Fatalf("content version %s missing from %+v", contentID, versions)
	return learningReviewVersion{}
}

// TestWorkshopLearningContentReviewGatesGenerationAndActivation drives the
// operator path end to end: unvalidated packages are refused, a draft carries no
// reviewer until it is approved, approval without activation does not change what
// members read, activation does, and active content cannot be retired in place.
func TestWorkshopLearningContentReviewGatesGenerationAndActivation(t *testing.T) {
	ctx := context.Background()
	fixture := newLearningReviewTest(t, "learning-review")
	collection := fixture.collection()

	status, body := requestJSON(t, http.MethodGet, collection, fixture.headers("read", ""), nil)
	if status != http.StatusOK {
		t.Fatalf("list learning content = %d: %s", status, body)
	}
	baseline := decodeLearningReviewVersions(t, body)
	if len(baseline) != 1 || baseline[0].Status != "approved" || !baseline[0].Active || !baseline[0].CatalogEnabled {
		t.Fatalf("fixture content is not the approved active version: %+v", baseline)
	}
	baselineID := baseline[0].ContentVersionID

	if guest, _ := requestJSON(t, http.MethodGet, collection, nil, nil); guest != http.StatusUnauthorized {
		t.Fatalf("guest review read = %d", guest)
	}
	if denied, _ := requestJSON(t, http.MethodGet, collection, fixture.headers("write", ""), nil); denied != http.StatusForbidden {
		t.Fatalf("write-only actor review read = %d", denied)
	}

	draft := learningReviewPackage(t, fixture.pool, fixture.bankID, "review-a", 3, false)
	if missing, _ := requestJSON(t, http.MethodPost, collection, fixture.headers("write", ""), json.RawMessage(draft)); missing != http.StatusBadRequest {
		t.Fatalf("import without idempotency key = %d", missing)
	}
	if broken, _ := requestJSON(t, http.MethodPost, collection, fixture.headers("write", "learning-review-import-broken"), json.RawMessage(learningReviewPackage(t, fixture.pool, fixture.bankID, "review-broken", 3, true))); broken != http.StatusBadRequest {
		t.Fatalf("import of a package with unknown question versions = %d", broken)
	}
	createStatus, createBody := requestJSON(t, http.MethodPost, collection, fixture.headers("write", "learning-review-import-a-001"), json.RawMessage(draft))
	if createStatus != http.StatusCreated {
		t.Fatalf("import draft = %d: %s", createStatus, createBody)
	}
	contentA := uuid.MustParse(operationResourceID(t, createBody))
	if replayStatus, replayBody := requestJSON(t, http.MethodPost, collection, fixture.headers("write", "learning-review-import-a-001"), json.RawMessage(draft)); replayStatus != createStatus || !bytes.Equal(replayBody, createBody) {
		t.Fatalf("import replay changed = %d %s", replayStatus, replayBody)
	}
	if againStatus, againBody := requestJSON(t, http.MethodPost, collection, fixture.headers("write", "learning-review-import-a-002"), json.RawMessage(draft)); againStatus != http.StatusCreated || operationResourceID(t, againBody) != contentA.String() {
		t.Fatalf("identical package became a second draft = %d %s", againStatus, againBody)
	}
	_, listBody := requestJSON(t, http.MethodGet, collection, fixture.headers("read", ""), nil)
	imported := learningReviewVersionByID(t, decodeLearningReviewVersions(t, listBody), contentA)
	if imported.Status != "draft" || imported.ReviewedBy != nil || imported.ReviewedAt != nil || imported.Active {
		t.Fatalf("imported draft carries review state: %+v", imported)
	}
	if imported.QuestionCount != 3 {
		t.Fatalf("imported draft question count = %d", imported.QuestionCount)
	}

	approveURL := collection + "/" + contentA.String() + "/approve"
	if denied, _ := requestJSON(t, http.MethodPost, approveURL, fixture.headers("write", "learning-review-approve-denied"), map[string]any{}); denied != http.StatusForbidden {
		t.Fatalf("import-only actor approval = %d", denied)
	}
	approveStatus, approveBody := requestJSON(t, http.MethodPost, approveURL, fixture.headers("publish", "learning-review-approve-a-001"), map[string]any{"note": "已核对来源与题干", "activate": false})
	if approveStatus != http.StatusOK {
		t.Fatalf("approve draft = %d: %s", approveStatus, approveBody)
	}
	_, listBody = requestJSON(t, http.MethodGet, collection, fixture.headers("read", ""), nil)
	approvedA := learningReviewVersionByID(t, decodeLearningReviewVersions(t, listBody), contentA)
	if approvedA.Status != "approved" || approvedA.Active || approvedA.ReviewedBy == nil || *approvedA.ReviewedBy != fixture.actorID || approvedA.ReviewedAt == nil {
		t.Fatalf("approval without activation = %+v", approvedA)
	}
	snapshot, err := fixture.service.BuildLearningEvidence(ctx, fixture.owner, fixture.bankID, time.Now())
	if err != nil || snapshot.ContentVersionID != baselineID {
		t.Fatalf("member read moved without activation: %s %v", snapshot.ContentVersionID, err)
	}
	var reviews int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_content_reviews WHERE content_version_id=$1 AND action='approve' AND actor_user_id=$2 AND note<>''`, contentA, fixture.actorID).Scan(&reviews); err != nil || reviews != 1 {
		t.Fatalf("approval audit rows = %d %v", reviews, err)
	}
	if double, _ := requestJSON(t, http.MethodPost, approveURL, fixture.headers("publish", "learning-review-approve-a-002"), map[string]any{"note": "再次审核"}); double != http.StatusConflict {
		t.Fatalf("second approval of the same draft = %d", double)
	}
	retireActive := collection + "/" + baselineID.String() + "/retire"
	if conflict, _ := requestJSON(t, http.MethodPost, retireActive, fixture.headers("publish", "learning-review-retire-active-001"), map[string]any{}); conflict != http.StatusConflict {
		t.Fatalf("retiring the active content in place = %d", conflict)
	}
	if unknown, _ := requestJSON(t, http.MethodPost, collection+"/"+uuid.NewString()+"/retire", fixture.headers("publish", "learning-review-retire-unknown-001"), map[string]any{}); unknown != http.StatusNotFound {
		t.Fatalf("retiring unknown content = %d", unknown)
	}

	draftB := learningReviewPackage(t, fixture.pool, fixture.bankID, "review-b", 3, false)
	createB, createBBody := requestJSON(t, http.MethodPost, collection, fixture.headers("write", "learning-review-import-b-001"), json.RawMessage(draftB))
	if createB != http.StatusCreated {
		t.Fatalf("import replacement = %d: %s", createB, createBBody)
	}
	contentB := uuid.MustParse(operationResourceID(t, createBBody))
	if promote, promoteBody := requestJSON(t, http.MethodPost, collection+"/"+contentB.String()+"/approve", fixture.headers("publish", "learning-review-approve-b-001"), map[string]any{"note": "替换旧内容", "activate": true, "enable": true}); promote != http.StatusOK {
		t.Fatalf("activate replacement = %d: %s", promote, promoteBody)
	}
	_, listBody = requestJSON(t, http.MethodGet, collection, fixture.headers("read", ""), nil)
	versions := decodeLearningReviewVersions(t, listBody)
	activeB := learningReviewVersionByID(t, versions, contentB)
	if !activeB.Active || !activeB.CatalogEnabled {
		t.Fatalf("activated replacement = %+v", activeB)
	}
	if stillActiveA := learningReviewVersionByID(t, versions, contentA); stillActiveA.Active {
		t.Fatalf("superseded content is still active: %+v", stillActiveA)
	}
	snapshot, err = fixture.service.BuildLearningEvidence(ctx, fixture.owner, fixture.bankID, time.Now())
	if err != nil || snapshot.ContentVersionID != contentB {
		t.Fatalf("member read did not move to the activated version: %s %v", snapshot.ContentVersionID, err)
	}
	if retire, retireBody := requestJSON(t, http.MethodPost, collection+"/"+contentA.String()+"/retire", fixture.headers("publish", "learning-review-retire-a-001"), map[string]any{"note": "已被替换"}); retire != http.StatusOK {
		t.Fatalf("retire superseded content = %d: %s", retire, retireBody)
	}
	_, listBody = requestJSON(t, http.MethodGet, collection, fixture.headers("read", ""), nil)
	if retired := learningReviewVersionByID(t, decodeLearningReviewVersions(t, listBody), contentA); retired.Status != "retired" {
		t.Fatalf("retired content status = %+v", retired)
	}
	if again, _ := requestJSON(t, http.MethodPost, collection+"/"+contentA.String()+"/retire", fixture.headers("publish", "learning-review-retire-a-002"), map[string]any{}); again != http.StatusConflict {
		t.Fatalf("retiring twice = %d", again)
	}
	// The stored operation result stays readable through the Workshop idempotency
	// route, which is what makes an operator retry safe after a lost response.
	operationURL := fmt.Sprintf("%s/api/v1/operations/approve_learning_content", fixture.url)
	operationStatus, operationBody := requestJSON(t, http.MethodGet, operationURL, fixture.headers("publish", "learning-review-approve-a-001"), nil)
	if operationStatus != http.StatusOK || !bytes.Contains(operationBody, []byte(contentA.String())) {
		t.Fatalf("stored approval operation = %d: %s", operationStatus, operationBody)
	}
}

// TestWorkshopLearningContentReviewConcurrentApprovalApprovesOnce pins the
// concurrency contract: two operators approving the same draft produce exactly
// one approval and one conflict, and only one audit row.
func TestWorkshopLearningContentReviewConcurrentApprovalApprovesOnce(t *testing.T) {
	ctx := context.Background()
	fixture := newLearningReviewTest(t, "learning-review-race")
	collection := fixture.collection()
	draft := learningReviewPackage(t, fixture.pool, fixture.bankID, "review-race", 3, false)
	createStatus, createBody := requestJSON(t, http.MethodPost, collection, fixture.headers("write", "learning-review-race-import"), json.RawMessage(draft))
	if createStatus != http.StatusCreated {
		t.Fatalf("import draft = %d: %s", createStatus, createBody)
	}
	contentID := uuid.MustParse(operationResourceID(t, createBody))
	approveURL := collection + "/" + contentID.String() + "/approve"

	codes := make([]int, 2)
	var group sync.WaitGroup
	for index := range codes {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			request, err := http.NewRequest(http.MethodPost, approveURL, bytes.NewReader(mustJSON(map[string]any{"note": "并发审核"})))
			if err != nil {
				codes[index] = -1
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Cookie", fixture.auth["publish"])
			request.Header.Set("Idempotency-Key", fmt.Sprintf("learning-review-race-approve-%d", index))
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				codes[index] = -2
				return
			}
			defer response.Body.Close()
			codes[index] = response.StatusCode
		}(index)
	}
	group.Wait()
	ok, conflicts := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("concurrent approval codes = %v", codes)
		}
	}
	if ok != 1 || conflicts != 1 {
		t.Fatalf("concurrent approvals = %v", codes)
	}
	var reviews int
	if err := fixture.pool.QueryRow(ctx, `SELECT count(*) FROM quizcraft_learning_content_reviews WHERE content_version_id=$1 AND action='approve'`, contentID).Scan(&reviews); err != nil || reviews != 1 {
		t.Fatalf("concurrent approval audit rows = %d %v", reviews, err)
	}
}
