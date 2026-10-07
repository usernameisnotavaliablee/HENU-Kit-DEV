package httpapi

// This is the joint Gateway <-> real-Core proof of the QuizCraft learning-report
// member chain. Everything below is one command:
//
//	QUIZCRAFT_JOINT_DATABASE_URL=postgres://mac@127.0.0.1:5432/postgres?sslmode=disable \
//	  go test ./internal/httpapi -run TestQuizCraftLearningReportMemberChainAcrossARealCore -v
//
// It is skipped unless QUIZCRAFT_JOINT_DATABASE_URL names a local PostgreSQL 16
// server, so `go test ./...` stays fast and offline. The URL is only the server
// (maintenance) connection: the test DROPS AND RECREATES the database named
// quizcraft_v2 on that server, because cmd/server refuses to serve any other
// database and a reused one makes service-replay nonces collide. The real Core
// binary is then built and exec'd, and the Gateway handler is built in-process
// against it, so every assertion below travels through the real HTTP contracts
// of both services rather than through a stub of either one.

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"henukit.dev/portal-gateway/internal/config"
	"henukit.dev/portal-gateway/internal/practice"
	"henukit.dev/portal-gateway/internal/session"
)

const (
	// The Gateway session key. The member cookie below is minted with exactly
	// this value, so a key mismatch cannot pass as an authenticated request.
	jointSessionKey         = "0123456789abcdef0123456789abcdef"
	jointSessionCookieName  = "henukit_portal_session_local"
	jointPortalOrigin       = "https://portal.test"
	jointCatalogClientID    = "portal-gateway"
	jointCatalogSecret      = "joint-catalog-secret-with-enough-entropy"
	jointCatalogKeyID       = "portal-catalog-key-1"
	jointCommandClientID    = "portal-gateway-command"
	jointCommandSecret      = "joint-command-secret-with-enough-entropy"
	jointCommandKeyID       = "portal-practice-command-key"
	jointPlatformSecret     = "joint-platform-secret-with-enough-entropy"
	jointEntitlementID      = "quizcraft-learning"
	jointEntitlementKeyID   = "learning-entitlement-key-1"
	jointEntitlementSecret  = "joint-entitlement-secret-with-enough-entropy"
	jointCoreAuthSecret     = "joint-core-auth-hmac-secret-with-enough-entropy"
	jointCoreCutoverSecret  = "joint-core-cutover-secret-with-enough-entropy"
	jointCoreProviderKey    = "sk-joint-core-provider-key-0001"
	jointCoreProviderModel  = "synthetic-joint-model-v1"
	jointV2Database         = "quizcraft_v2"
	jointEntitlementPrefix  = "/api/v1/internal/quizcraft/entitlements/"
	jointMigrationsRelative = "../../../../products/quizcraft/go-service/db/migrations"
	jointCoreDirRelative    = "../../../../products/quizcraft/go-service"
	jointFixtureRelative    = "testdata/quizcraft_learning_report_fixture.sql"
)

// jointAssertions counts the assertions that actually ran, so a passing run can
// report how much of the chain it proved instead of how much it printed.
type jointAssertions struct{ passed int }

func (a *jointAssertions) eq(t *testing.T, label string, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s = %#v, want %#v", label, got, want)
	}
	a.passed++
}

func (a *jointAssertions) ok(t *testing.T, label string, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf("%s: %s", label, fmt.Sprintf(format, args...))
	}
	a.passed++
}

// jointEnvelope reads either a Core/Gateway success envelope or their flat
// error envelope; both carry request_id, and a member surface must never be
// asserted against a body that has neither.
type jointEnvelope struct {
	RequestID string          `json:"request_id"`
	Data      json.RawMessage `json:"data"`
	Error     string          `json:"error"`
	Message   string          `json:"message"`
}

func jointDecode(t *testing.T, label string, body []byte) jointEnvelope {
	t.Helper()
	var envelope jointEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("%s is not a JSON envelope (%v): %s", label, err, body)
	}
	return envelope
}

func (e jointEnvelope) data(t *testing.T, label string) map[string]any {
	t.Helper()
	if len(e.Data) == 0 || string(e.Data) == "null" {
		t.Fatalf("%s has no data: %s", label, e.Data)
	}
	var value map[string]any
	if err := json.Unmarshal(e.Data, &value); err != nil {
		t.Fatalf("%s data is not an object (%v): %s", label, err, e.Data)
	}
	return value
}

func jointUUID(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("crypto/rand: %v", err)
	}
	raw[6] = (raw[6] & 0x0f) | 0x40
	raw[8] = (raw[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", raw[0:4], raw[4:6], raw[6:8], raw[8:10], raw[10:16])
}

func jointAbsPath(t *testing.T, relative string) string {
	t.Helper()
	absolute, err := filepath.Abs(relative)
	if err != nil {
		t.Fatalf("resolve %s: %v", relative, err)
	}
	return absolute
}

// jointEnv builds a child environment that replaces exactly the keys given,
// instead of appending duplicates an exec'd process could resolve either way.
func jointEnv(overrides map[string]string) []string {
	blocked := make(map[string]bool, len(overrides))
	for key := range overrides {
		blocked[key] = true
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		if name, _, found := strings.Cut(entry, "="); found && blocked[name] {
			continue
		}
		env = append(env, entry)
	}
	keys := make([]string, 0, len(overrides))
	for key := range overrides {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

func jointPSQL(t *testing.T, databaseURL string, args ...string) (string, error) {
	t.Helper()
	psql, err := exec.LookPath("psql")
	if err != nil {
		t.Fatalf("psql is required to create and migrate the joint PostgreSQL database: %v", err)
	}
	command := exec.Command(psql, append([]string{databaseURL, "-X", "-q", "-v", "ON_ERROR_STOP=1"}, args...)...)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	runErr := command.Run()
	return output.String(), runErr
}

// jointRefuseToDestroyExistingSchema makes the destructive part of this test
// explicit. The test only ever wants a scratch database, so if quizcraft_v2
// already holds QuizCraft tables the run stops unless the operator says the data
// is disposable. A database psql cannot connect to is not a conflict: that is
// exactly the scratch case this test creates for itself.
func jointRefuseToDestroyExistingSchema(t *testing.T, adminURL string) {
	t.Helper()
	if strings.TrimSpace(os.Getenv("QUIZCRAFT_JOINT_ALLOW_DESTRUCTIVE_RECREATE")) == "1" {
		return
	}
	parsed, err := url.Parse(strings.TrimSpace(adminURL))
	if err != nil {
		return
	}
	target := *parsed
	target.Path = "/" + jointV2Database
	output, err := jointPSQL(t, target.String(), "-Atc",
		`SELECT count(*) FROM pg_class WHERE relname LIKE 'quizcraft\_%'`)
	if err != nil {
		return
	}
	count, convErr := strconv.Atoi(strings.TrimSpace(output))
	if convErr != nil || count == 0 {
		return
	}
	t.Fatalf("quizcraft_v2 already holds %d QuizCraft tables and this test drops and recreates it; re-run with QUIZCRAFT_JOINT_ALLOW_DESTRUCTIVE_RECREATE=1 if that data is disposable", count)
}

// jointFreshDatabase drops and recreates the one database name the real Core
// binary accepts, then returns its connection URL.
func jointFreshDatabase(t *testing.T, adminURL string) string {
	t.Helper()
	parsed, err := url.Parse(strings.TrimSpace(adminURL))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatalf("QUIZCRAFT_JOINT_DATABASE_URL must be a postgres:// URL, got %q", adminURL)
	}
	jointRefuseToDestroyExistingSchema(t, adminURL)
	target := *parsed
	target.Path = "/" + jointV2Database
	for _, statement := range []string{
		"DROP DATABASE IF EXISTS " + jointV2Database + " WITH (FORCE)",
		"CREATE DATABASE " + jointV2Database,
	} {
		if output, err := jointPSQL(t, adminURL, "-c", statement); err != nil {
			t.Fatalf("psql %q failed: %v\n%s", statement, err, output)
		}
	}
	t.Cleanup(func() {
		// Leaving the scratch database behind breaks the Core integration tests
		// that create their own isolated quizcraft_v2: they fail on "already
		// exists" before they test anything. Registered first, so it runs after
		// the Core process and the server are already gone.
		if output, err := jointPSQL(t, adminURL, "-c", "DROP DATABASE IF EXISTS "+jointV2Database+" WITH (FORCE)"); err != nil {
			t.Logf("dropping %s after the joint run failed: %v\n%s", jointV2Database, err, output)
		}
	})
	t.Logf("joint run: recreated local database %s on %s", jointV2Database, parsed.Host)
	return target.String()
}

func jointApplyMigrations(t *testing.T, databaseURL, migrationsDir string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(migrationsDir, "*.up.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no QuizCraft migrations found in %s: %v", migrationsDir, err)
	}
	sort.Strings(files)
	args := make([]string, 0, 2*len(files))
	for _, file := range files {
		args = append(args, "-f", file)
	}
	// Two passes: the Core's second pass completes objects the first pass can
	// only create, and it is also what the Core's own integration suite does.
	for pass := 1; pass <= 2; pass++ {
		if output, err := jointPSQL(t, databaseURL, args...); err != nil {
			t.Fatalf("applying %d QuizCraft migrations (pass %d) failed: %v\n%s", len(files), pass, err, output)
		}
	}
	t.Logf("joint run: applied %d migrations twice", len(files))
}

func jointSeedFixture(t *testing.T, databaseURL, fixture string, values map[string]string) {
	t.Helper()
	args := []string{"-f", fixture}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		args = append(args, "-v", key+"="+values[key])
	}
	if output, err := jointPSQL(t, databaseURL, args...); err != nil {
		t.Fatalf("seeding the learning-report fixture failed: %v\n%s", err, output)
	}
}

// jointLearningDocument mirrors the Core's LearningContentDocument field order.
// The Core re-derives the content digest from its own canonical re-marshal of
// the stored document, so the fixture must receive bytes this encoder produces.
type jointLearningTag struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	Definition string `json:"definition"`
}

type jointLearningQuestion struct {
	QuestionID        string   `json:"question_id"`
	QuestionVersionID string   `json:"question_version_id"`
	TagIDs            []string `json:"tag_ids"`
}

type jointLearningSource struct {
	ID string `json:"id"`
}

type jointLearningLesson struct {
	ID string `json:"id"`
}

type jointLearningDocument struct {
	SchemaVersion int                     `json:"schema_version"`
	Tags          []jointLearningTag      `json:"tags"`
	Questions     []jointLearningQuestion `json:"questions"`
	Sources       []jointLearningSource   `json:"sources"`
	Lessons       []jointLearningLesson   `json:"lessons"`
}

type jointFixtureIDs struct {
	bank            string
	bankVersion     string
	contentVersion  string
	reviewer        string
	question        [2]string
	questionVersion [2]string
	session         string
	attempt         string
	chainUser       string
	limitUser       string
	revokedUser     string
}

func newJointFixtureIDs(t *testing.T) jointFixtureIDs {
	t.Helper()
	return jointFixtureIDs{
		bank:            jointUUID(t),
		bankVersion:     jointUUID(t),
		contentVersion:  jointUUID(t),
		reviewer:        jointUUID(t),
		question:        [2]string{jointUUID(t), jointUUID(t)},
		questionVersion: [2]string{jointUUID(t), jointUUID(t)},
		session:         jointUUID(t),
		attempt:         jointUUID(t),
		chainUser:       jointUUID(t),
		limitUser:       jointUUID(t),
		revokedUser:     jointUUID(t),
	}
}

func (ids jointFixtureIDs) document(t *testing.T) string {
	t.Helper()
	raw, err := json.Marshal(jointLearningDocument{
		SchemaVersion: 1,
		Tags: []jointLearningTag{
			{ID: "math", Kind: "knowledge", Name: "算术", Definition: "基础运算"},
			{ID: "trace", Kind: "ability", Name: "过程追踪", Definition: "追踪表达式求值"},
		},
		Questions: []jointLearningQuestion{
			{QuestionID: ids.question[0], QuestionVersionID: ids.questionVersion[0], TagIDs: []string{"math", "trace"}},
			{QuestionID: ids.question[1], QuestionVersionID: ids.questionVersion[1], TagIDs: []string{"trace"}},
		},
	})
	if err != nil {
		t.Fatalf("marshal learning content document: %v", err)
	}
	return string(raw)
}

func (ids jointFixtureIDs) sqlValues(t *testing.T) map[string]string {
	t.Helper()
	return map[string]string{
		"bank_id":               ids.bank,
		"bank_version_id":       ids.bankVersion,
		"content_version_id":    ids.contentVersion,
		"reviewer_id":           ids.reviewer,
		"question_id_1":         ids.question[0],
		"question_version_id_1": ids.questionVersion[0],
		"question_id_2":         ids.question[1],
		"question_version_id_2": ids.questionVersion[1],
		"session_id":            ids.session,
		"attempt_id":            ids.attempt,
		"chain_user_id":         ids.chainUser,
		"document":              ids.document(t),
	}
}

// jointEntitlementStub is the platform entitlement service the Core calls. It
// VERIFIES the Core's HMAC instead of accepting anything, so a passing chain
// proves the signed caller, not just a reachable port.
type jointEntitlementStub struct {
	server     *httptest.Server
	lifetime   atomic.Bool
	calls      atomic.Int32
	mu         sync.Mutex
	violations []string
	checked    map[string]int
}

func newJointEntitlementStub(t *testing.T, lifetime bool) *jointEntitlementStub {
	t.Helper()
	stub := &jointEntitlementStub{checked: map[string]int{}}
	stub.lifetime.Store(lifetime)
	stub.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		attempt := stub.calls.Add(1)
		reject := func(format string, args ...any) {
			stub.mu.Lock()
			stub.violations = append(stub.violations, fmt.Sprintf(format, args...))
			stub.mu.Unlock()
			writer.WriteHeader(http.StatusUnauthorized)
		}
		if request.Method != http.MethodGet {
			reject("entitlement stub method = %s", request.Method)
			return
		}
		if !strings.HasPrefix(request.URL.Path, jointEntitlementPrefix) {
			reject("entitlement stub path = %s", request.URL.Path)
			return
		}
		userID := strings.TrimPrefix(request.URL.Path, jointEntitlementPrefix)
		if !practice.ValidUUID(userID) {
			reject("entitlement stub user id = %q", userID)
			return
		}
		clientID, secret, basic := request.BasicAuth()
		if !basic || clientID != jointEntitlementID || secret != jointEntitlementSecret {
			reject("entitlement stub Basic auth = %t %q", basic, clientID)
			return
		}
		if request.Header.Get("X-Service-Id") != clientID || request.Header.Get("X-Key-Id") != jointEntitlementKeyID {
			reject("entitlement stub identity headers = %q/%q", request.Header.Get("X-Service-Id"), request.Header.Get("X-Key-Id"))
			return
		}
		if request.Header.Get("X-Actor-User-Id") != userID {
			reject("entitlement stub actor = %q, path user = %q", request.Header.Get("X-Actor-User-Id"), userID)
			return
		}
		timestamp := request.Header.Get("X-Timestamp")
		issued, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || math.Abs(float64(time.Now().Unix()-issued)) > 300 {
			reject("entitlement stub timestamp = %q", timestamp)
			return
		}
		nonce := request.Header.Get("X-Nonce")
		decoded, err := base64.RawURLEncoding.DecodeString(nonce)
		if err != nil || len(decoded) != 24 {
			reject("entitlement stub nonce = %q", nonce)
			return
		}
		digest := sha256.Sum256(nil)
		canonical := strings.Join([]string{http.MethodGet, request.URL.RequestURI(), timestamp, nonce, hex.EncodeToString(digest[:]), userID}, "\n")
		mac := hmac.New(sha256.New, []byte(jointEntitlementSecret))
		_, _ = mac.Write([]byte(canonical))
		want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(request.Header.Get("X-Signature")), []byte(want)) {
			reject("entitlement stub signature does not bind the Core identity")
			return
		}
		stub.mu.Lock()
		stub.checked[userID]++
		stub.mu.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(writer, `{"data":{"lifetime":%t,"version":1},"request_id":"req_joint_entitlement_%d"}`, stub.lifetime.Load(), attempt)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (stub *jointEntitlementStub) url() string { return stub.server.URL }

func (stub *jointEntitlementStub) checkedFor(userID string) int {
	stub.mu.Lock()
	defer stub.mu.Unlock()
	return stub.checked[userID]
}

func (stub *jointEntitlementStub) assertClean(t *testing.T) {
	t.Helper()
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.violations) != 0 {
		t.Fatalf("the Core's entitlement requests violated the signed contract: %s", strings.Join(stub.violations, "; "))
	}
}

// jointPlatformStub answers the Gateway's per-request Platform Core permission
// check. The real Core never sees it: it only gates the Gateway's member reads.
type jointPlatformStub struct {
	server     *httptest.Server
	calls      atomic.Int32
	mu         sync.Mutex
	violations []string
}

func newJointPlatformStub(t *testing.T) *jointPlatformStub {
	t.Helper()
	stub := &jointPlatformStub{}
	stub.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		stub.calls.Add(1)
		clientID, _, basic := request.BasicAuth()
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/authorization/check" || !basic || clientID != "portal-gateway" {
			stub.mu.Lock()
			stub.violations = append(stub.violations, fmt.Sprintf("platform check = %s %s (basic %t %q)", request.Method, request.URL.Path, basic, clientID))
			stub.mu.Unlock()
			writer.WriteHeader(http.StatusInternalServerError)
			return
		}
		writer.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(stub.server.Close)
	return stub
}

func (stub *jointPlatformStub) url() string { return stub.server.URL }

func (stub *jointPlatformStub) assertClean(t *testing.T) {
	t.Helper()
	stub.mu.Lock()
	defer stub.mu.Unlock()
	if len(stub.violations) != 0 {
		t.Fatalf("Gateway Platform Core permission checks were malformed: %s", strings.Join(stub.violations, "; "))
	}
}

// jointLog collects a child process's output for failure diagnostics without
// racing the test goroutine.
type jointLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (log *jointLog) Write(value []byte) (int, error) {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.buf.Write(value)
}

func (log *jointLog) String() string {
	log.mu.Lock()
	defer log.mu.Unlock()
	return log.buf.String()
}

func jointFreePort(t *testing.T) int {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve a free port: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	_ = listener.Close()
	return port
}

// jointBuildCore builds the real Core binary once per test run.
func jointBuildCore(t *testing.T, coreDir string) string {
	t.Helper()
	cacheDir := strings.TrimSpace(os.Getenv("GOCACHE"))
	if cacheDir == "" {
		// <repo>/products/quizcraft/go-service -> <repo>
		repoRoot := filepath.Dir(filepath.Dir(filepath.Dir(coreDir)))
		cacheDir = filepath.Join(repoRoot, ".cache", "go-build")
	}
	binary := filepath.Join(t.TempDir(), "quizcraft-core")
	command := exec.Command("go", "build", "-o", binary, "./cmd/server")
	command.Dir = coreDir
	command.Env = jointEnv(map[string]string{"GOFLAGS": "-mod=mod", "GOCACHE": cacheDir})
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		t.Fatalf("go build ./cmd/server failed: %v\n%s", err, output.String())
	}
	return binary
}

// jointCoreProcess is one exec'd real Core binary.
type jointCoreProcess struct {
	baseURL string
	log     *jointLog
}

// jointStartCore execs the built binary with Dir set to the Core module and the
// env the Core needs, then waits until it answers /readyz.
func jointStartCore(t *testing.T, binary, coreDir string, env map[string]string) *jointCoreProcess {
	t.Helper()
	port := jointFreePort(t)
	env["QUIZCRAFT_HTTP_ADDR"] = fmt.Sprintf("127.0.0.1:%d", port)
	address := fmt.Sprintf("http://127.0.0.1:%d", port)
	log := &jointLog{}
	command := exec.Command(binary)
	command.Dir = coreDir
	command.Env = jointEnv(env)
	command.Stdout, command.Stderr = log, log
	if err := command.Start(); err != nil {
		t.Fatalf("start the real QuizCraft Core: %v", err)
	}
	exited := make(chan struct{})
	go func() {
		_ = command.Wait()
		close(exited)
	}()
	t.Cleanup(func() {
		_ = command.Process.Kill()
		<-exited
	})
	deadline := time.Now().Add(30 * time.Second)
	for {
		response, err := http.Get(address + "/readyz")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				t.Logf("joint run: real QuizCraft Core ready at %s", address)
				return &jointCoreProcess{baseURL: address, log: log}
			}
		}
		select {
		case <-exited:
			t.Fatalf("the real QuizCraft Core exited before it was ready (%v)\n%s", err, log.String())
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("the real QuizCraft Core never answered /readyz at %s (%v)\n%s", address, err, log.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// jointCoreEnv is the operator configuration the real Core binary reads.
func jointCoreEnv(databaseURL string, values map[string]string) map[string]string {
	env := map[string]string{
		"DATABASE_URL":                             databaseURL,
		"QUIZCRAFT_AUTH_HMAC_SECRET":               jointCoreAuthSecret,
		"QUIZCRAFT_CUTOVER_EVIDENCE_SECRET":        jointCoreCutoverSecret,
		"QUIZCRAFT_WRITES_ENABLED":                 "1",
		"QUIZCRAFT_PORTAL_COMMANDS_ENABLED":        "1",
		"QUIZCRAFT_PORTAL_CATALOG_CLIENT_ID":       jointCatalogClientID,
		"QUIZCRAFT_PORTAL_CATALOG_KEY_ID":          jointCatalogKeyID,
		"QUIZCRAFT_PORTAL_CATALOG_CLIENT_SECRET":   jointCatalogSecret,
		"QUIZCRAFT_PORTAL_COMMAND_CLIENT_ID":       jointCommandClientID,
		"QUIZCRAFT_PORTAL_COMMAND_KEY_ID":          jointCommandKeyID,
		"QUIZCRAFT_PORTAL_COMMAND_CLIENT_SECRET":   jointCommandSecret,
		"QUIZCRAFT_LEARNING_ENTITLEMENT_CLIENT_ID": jointEntitlementID,
		"QUIZCRAFT_LEARNING_ENTITLEMENT_KEY_ID":    jointEntitlementKeyID,
		"QUIZCRAFT_LEARNING_ENTITLEMENT_SECRET":    jointEntitlementSecret,
		"QUIZCRAFT_LEARNING_WORKER_ENABLED":        "1",
		"QUIZCRAFT_LEARNING_WORKER_POLL":           "10m",
		"QUIZCRAFT_LEARNING_SCHEDULER_INTERVAL":    "0",
		"QUIZCRAFT_LEARNING_PROVIDER_URL":          "http://127.0.0.1:9/v1",
		"QUIZCRAFT_LEARNING_PROVIDER_API_KEY":      jointCoreProviderKey,
		"QUIZCRAFT_LEARNING_PROVIDER_MODEL":        jointCoreProviderModel,
	}
	for key, value := range values {
		env[key] = value
	}
	return env
}

func newJointHandler(t *testing.T, platformURL, coreURL string, learningReportsEnabled bool) *Handler {
	t.Helper()
	handler, err := New(config.Config{
		SessionKey:                      []byte(jointSessionKey),
		LocalSessionCookieName:          jointSessionCookieName,
		PlatformCoreURL:                 platformURL,
		PlatformClientID:                jointCatalogClientID,
		PlatformSecret:                  jointPlatformSecret,
		PlatformKeyID:                   "platform-key-1",
		PortalRedirectURI:               jointPortalOrigin + "/api/v1/auth/callback",
		PortalOrigin:                    jointPortalOrigin,
		PortalAPIURL:                    "http://127.0.0.1:9",
		PracticeURL:                     coreURL,
		PracticeAuth:                    config.ServiceAuth{ClientID: jointCatalogClientID, ClientSecret: jointCatalogSecret, KeyID: jointCatalogKeyID},
		PracticeCommandAuth:             config.ServiceAuth{ClientID: jointCommandClientID, ClientSecret: jointCommandSecret, KeyID: jointCommandKeyID},
		PracticeCommandsEnabled:         true,
		QuizCraftCatalogEnabled:         true,
		QuizCraftV2ReadsEnabled:         true,
		QuizCraftCoreURL:                coreURL,
		QuizCraftCoreAuth:               config.ServiceAuth{ClientID: jointCatalogClientID, ClientSecret: jointCatalogSecret, KeyID: jointCatalogKeyID},
		QuizCraftLearningReportsEnabled: learningReportsEnabled,
	}, nil)
	if err != nil {
		t.Fatalf("build the in-process Gateway handler: %v", err)
	}
	return handler
}

// jointMemberCookie mints a lifetime member session with the Gateway's own
// SessionKey, so an unauthenticated request cannot be mistaken for a member.
func jointMemberCookie(t *testing.T, userID string) *http.Cookie {
	t.Helper()
	codec, err := session.NewCodec([]byte(jointSessionKey))
	if err != nil {
		t.Fatalf("session.NewCodec: %v", err)
	}
	encoded, err := codec.Encode(session.Value{
		UserID:        userID,
		DisplayName:   "联合链路会员",
		ExchangeToken: strings.Repeat("x", 32),
		ExpiresAt:     time.Now().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("encode the member session: %v", err)
	}
	return &http.Cookie{Name: jointSessionCookieName, Value: encoded}
}

// jointCall drives the member chain over the Gateway router. The Gateway itself
// is the only in-process hop; every Core request below it is real HTTP.
func jointCall(t *testing.T, handler *Handler, method, path, body string, cookie *http.Cookie, idempotencyKey string) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, "http://portal.test"+path, reader)
	request.Header.Set("X-Request-Id", "req_joint_"+strings.ReplaceAll(jointUUID(t), "-", ""))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if cookie != nil {
		request.AddCookie(cookie)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	recorder := httptest.NewRecorder()
	handler.Router().ServeHTTP(recorder, request)
	return recorder.Code, recorder.Body.Bytes()
}

func jointLearningPath(bankID, suffix string) string {
	return "/api/v1/practice/banks/" + bankID + "/learning-reports" + suffix
}

func jointPreferencesBody(enabled bool, goal string, chapters []string) string {
	if chapters == nil {
		chapters = []string{}
	}
	encoded, err := json.Marshal(map[string]any{
		"enabled":                   enabled,
		"interval_days":             7,
		"goal":                      goal,
		"chapter_ids":               chapters,
		"external_analysis_consent": enabled,
	})
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

// TestQuizCraftLearningReportMemberChainAcrossARealCore proves the member
// learning-report chain against a real Core process, and then proves the three
// documented negatives around it.
func TestQuizCraftLearningReportMemberChainAcrossARealCore(t *testing.T) {
	adminURL := strings.TrimSpace(os.Getenv("QUIZCRAFT_JOINT_DATABASE_URL"))
	if adminURL == "" {
		// This is the only PostgreSQL-backed evidence for the learning-report
		// chain, so a job that is supposed to run it must fail rather than count a
		// skip as a pass. A developer without PostgreSQL still gets the skip.
		if os.Getenv("QUIZCRAFT_JOINT_REQUIRED") == "1" {
			t.Fatal("QUIZCRAFT_JOINT_REQUIRED=1 but QUIZCRAFT_JOINT_DATABASE_URL is unset: the joint real-Core learning-report chain is required here and must not be skipped")
		}
		t.Skip("set QUIZCRAFT_JOINT_DATABASE_URL (a local postgres:// maintenance URL) to run the joint real-Core learning-report chain")
	}

	migrationsDir := jointAbsPath(t, jointMigrationsRelative)
	coreDir := jointAbsPath(t, jointCoreDirRelative)
	fixture := jointAbsPath(t, jointFixtureRelative)
	if _, err := os.Stat(filepath.Join(coreDir, "go.mod")); err != nil {
		t.Fatalf("the Gateway joint test needs the Core module at %s: %v", coreDir, err)
	}

	ids := newJointFixtureIDs(t)
	databaseURL := jointFreshDatabase(t, adminURL)
	jointApplyMigrations(t, databaseURL, migrationsDir)
	jointSeedFixture(t, databaseURL, fixture, ids.sqlValues(t))

	entitlement := newJointEntitlementStub(t, true)
	platform := newJointPlatformStub(t)
	binary := jointBuildCore(t, coreDir)
	core := jointStartCore(t, binary, coreDir, jointCoreEnv(databaseURL, map[string]string{
		"QUIZCRAFT_LEARNING_ENTITLEMENT_URL": entitlement.url(),
	}))
	member := newJointHandler(t, platform.url(), core.baseURL, true)
	chainCookie := jointMemberCookie(t, ids.chainUser)

	var checks jointAssertions
	defer func() {
		t.Logf("joint member chain: %d assertions passed", checks.passed)
	}()

	t.Run("member chain over real HTTP", func(t *testing.T) {
		platformCallsBefore := platform.calls.Load()

		// 1. The member practice catalog still serves the published course.
		status, body := jointCall(t, member, http.MethodGet, "/api/v1/practice/catalog", "", nil, "")
		checks.eq(t, "practice catalog status", status, http.StatusOK)
		catalog := jointDecode(t, "catalog", body)
		checks.ok(t, "practice catalog request id", strings.TrimSpace(catalog.RequestID) != "", "body = %s", body)
		var catalogData struct {
			Banks []struct {
				BankID        string `json:"bank_id"`
				BankVersionID string `json:"bank_version_id"`
				QuestionCount int    `json:"question_count"`
				Chapters      []struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				} `json:"chapters"`
			} `json:"banks"`
		}
		if err := json.Unmarshal(body, &catalogData); err != nil {
			t.Fatalf("practice catalog is not the generated contract (%v): %s", err, body)
		}
		checks.ok(t, "practice catalog has banks", len(catalogData.Banks) >= 1, "body = %s", body)
		found, chapters := false, map[string]string{}
		for _, bank := range catalogData.Banks {
			if bank.BankID == ids.bank {
				found = true
				checks.eq(t, "catalog question count", bank.QuestionCount, 2)
				checks.eq(t, "catalog bank version", bank.BankVersionID, ids.bankVersion)
				for _, chapter := range bank.Chapters {
					chapters[chapter.ID] = chapter.Name
				}
			}
		}
		checks.ok(t, "catalog serves the fixture course", found, "bank %s missing from %s", ids.bank, body)
		checks.eq(t, "catalog chapter ch01", chapters["ch01"], "基础运算")

		// 2. A member with no stored preferences reads the Core's disabled defaults.
		status, body = jointCall(t, member, http.MethodGet, jointLearningPath(ids.bank, "/preferences"), "", chainCookie, "")
		checks.eq(t, "preferences read status", status, http.StatusOK)
		preferences := jointDecode(t, "preferences", body).data(t, "preferences")
		checks.eq(t, "default preferences disabled", preferences["enabled"], false)
		checks.eq(t, "default preferences interval", preferences["interval_days"], float64(7))
		checks.eq(t, "default preferences goal", preferences["goal"], "follow_course")
		checks.eq(t, "default preferences scope", preferences["chapter_ids"], []any{})
		checks.eq(t, "default preferences consent", preferences["external_analysis_consent"], false)
		checks.eq(t, "default preferences bank", preferences["bank_id"], ids.bank)

		// 3. Enabling generation and consent is a live-membership decision.
		status, body = jointCall(t, member, http.MethodPut, jointLearningPath(ids.bank, "/preferences"),
			jointPreferencesBody(true, "follow_course", []string{}), chainCookie, "joint-chain-prefs-0001")
		checks.eq(t, "preferences write status", status, http.StatusOK)
		enabled := jointDecode(t, "preferences write", body).data(t, "preferences write")
		checks.eq(t, "preferences write enabled", enabled["enabled"], true)
		checks.eq(t, "preferences write consent", enabled["external_analysis_consent"], true)
		checks.eq(t, "preferences write revision", enabled["revision"], float64(2))
		checks.ok(t, "preferences write arms the schedule", enabled["next_due_at"] != nil, "body = %s", body)

		// 4. Generating queues work instead of running the model in the request.
		status, body = jointCall(t, member, http.MethodPost, jointLearningPath(ids.bank, ""), "", chainCookie, "joint-chain-generate-0001")
		checks.eq(t, "generate status", status, http.StatusAccepted)
		task := jointDecode(t, "generate", body).data(t, "generate")
		checks.eq(t, "generate task status", task["status"], "queued")
		checks.eq(t, "generate task bank", task["bank_id"], ids.bank)
		taskID, isString := task["task_id"].(string)
		checks.ok(t, "generate returns a task id", isString && strings.TrimSpace(taskID) != "", "body = %s", body)

		// 5. The owner can read that task's progress.
		status, body = jointCall(t, member, http.MethodGet, jointLearningPath(ids.bank, "/tasks/"+taskID), "", chainCookie, "")
		checks.eq(t, "task read status", status, http.StatusOK)
		progress := jointDecode(t, "task read", body).data(t, "task read")
		checks.eq(t, "task read task id", progress["task_id"], taskID)
		checks.eq(t, "task read status value", progress["status"], "queued")

		// 6. Clearing withdraws the derived work for this owner and course.
		status, body = jointCall(t, member, http.MethodDelete, jointLearningPath(ids.bank, ""), "", chainCookie, "joint-chain-clear-0001")
		checks.eq(t, "clear status", status, http.StatusOK)
		cleared := jointDecode(t, "clear", body).data(t, "clear")
		checks.eq(t, "clear result", cleared["cleared"], true)
		checks.ok(t, "clear bumps the revision", cleared["revision"] != nil, "body = %s", body)

		entitlement.assertClean(t)
		checks.ok(t, "Core asked the signed entitlement service for this member", entitlement.checkedFor(ids.chainUser) >= 2,
			"checked = %d", entitlement.checkedFor(ids.chainUser))
		platform.assertClean(t)
		checks.eq(t, "member reads checked Portal permission", platform.calls.Load()-platformCallsBefore, int32(2))
	})

	t.Run("the dark gate answers an honest 503 without dependencies", func(t *testing.T) {
		platformCallsBefore := platform.calls.Load()
		dark := newJointHandler(t, platform.url(), core.baseURL, false)
		status, body := jointCall(t, dark, http.MethodGet, jointLearningPath(ids.bank, "/preferences"), "", chainCookie, "")
		checks.eq(t, "dark preferences read status", status, http.StatusServiceUnavailable)
		checks.eq(t, "dark preferences read error", jointDecode(t, "dark read", body).Error, "practice learning reports are not enabled")
		checks.ok(t, "dark read serves no report data", !bytes.Contains(body, []byte(ids.bank)), "body = %s", body)
		checks.eq(t, "dark read checked no Portal permission", platform.calls.Load()-platformCallsBefore, int32(0))
	})

	t.Run("a revoked membership cannot enable generation", func(t *testing.T) {
		entitlement.lifetime.Store(false)
		t.Cleanup(func() { entitlement.lifetime.Store(true) })
		revokedCookie := jointMemberCookie(t, ids.revokedUser)
		status, body := jointCall(t, member, http.MethodPut, jointLearningPath(ids.bank, "/preferences"),
			jointPreferencesBody(true, "follow_course", []string{}), revokedCookie, "joint-revoked-enable-0001")
		checks.eq(t, "revoked enable status", status, http.StatusForbidden)
		// Core's own reason reaches the member instead of the practice-flavoured
		// fallback, which is the whole point of forwarding the code.
		checks.eq(t, "revoked enable error", jointDecode(t, "revoked enable", body).Error, "learning_entitlement_required")
		entitlement.assertClean(t)
		checks.eq(t, "the Core checked the revoked member's lifetime", entitlement.checkedFor(ids.revokedUser), 1)

		// The member's settings must not have been written by a refused request.
		status, body = jointCall(t, member, http.MethodGet, jointLearningPath(ids.bank, "/preferences"), "", revokedCookie, "")
		checks.eq(t, "revoked member may still read settings", status, http.StatusOK)
		checks.eq(t, "refused enable stored nothing", jointDecode(t, "revoked read", body).data(t, "revoked read")["enabled"], false)

		// The newest report and task progress are lifetime-gated inside Core, so a
		// revoked member must be told the membership lapsed rather than that the
		// feature is temporarily broken. Preferences stay readable on purpose.
		status, body = jointCall(t, member, http.MethodGet, jointLearningPath(ids.bank, "/latest"), "", revokedCookie, "")
		checks.eq(t, "revoked member's newest report read", status, http.StatusForbidden)
		checks.eq(t, "revoked newest report read error", jointDecode(t, "revoked latest", body).Error, "learning_entitlement_required")
		// The task id never has to exist: Core checks the membership before it
		// looks the task up, which is exactly the ordering this assertion relies on.
		status, body = jointCall(t, member, http.MethodGet, jointLearningPath(ids.bank, "/tasks/"+jointUUID(t)), "", revokedCookie, "")
		checks.eq(t, "revoked member's task read", status, http.StatusForbidden)
		checks.eq(t, "revoked task read error", jointDecode(t, "revoked task", body).Error, "learning_entitlement_required")
	})

	t.Run("the manual generation guard refuses only new model work", func(t *testing.T) {
		limited := jointStartCore(t, binary, coreDir, jointCoreEnv(databaseURL, map[string]string{
			"QUIZCRAFT_LEARNING_ENTITLEMENT_URL": entitlement.url(),
			"QUIZCRAFT_LEARNING_MANUAL_LIMIT":    "1",
		}))
		guarded := newJointHandler(t, platform.url(), limited.baseURL, true)
		limitCookie := jointMemberCookie(t, ids.limitUser)

		status, body := jointCall(t, guarded, http.MethodPut, jointLearningPath(ids.bank, "/preferences"),
			jointPreferencesBody(true, "follow_course", []string{}), limitCookie, "joint-limit-prefs-0001")
		checks.eq(t, "guard preferences write status", status, http.StatusOK)
		first := jointDecode(t, "guard preferences", body).data(t, "guard preferences")
		checks.eq(t, "guard preferences enabled", first["enabled"], true)

		status, body = jointCall(t, guarded, http.MethodPost, jointLearningPath(ids.bank, ""), "", limitCookie, "joint-limit-generate-0001")
		checks.eq(t, "first generation inside the window", status, http.StatusAccepted)
		checks.eq(t, "first generation queued", jointDecode(t, "first generate", body).data(t, "first generate")["status"], "queued")

		// Different effective input: a narrower scope is a new revision, so it is
		// new model work rather than a replay of the request above.
		status, body = jointCall(t, guarded, http.MethodPut, jointLearningPath(ids.bank, "/preferences"),
			jointPreferencesBody(true, "follow_course", []string{"ch01"}), limitCookie, "joint-limit-prefs-0002")
		checks.eq(t, "guard scope change status", status, http.StatusOK)
		changed := jointDecode(t, "guard scope change", body).data(t, "guard scope change")
		before, beforeOK := first["revision"].(float64)
		after, afterOK := changed["revision"].(float64)
		checks.ok(t, "guard scope change is a new revision", beforeOK && afterOK && after > before,
			"revision was %v, now %v", first["revision"], changed["revision"])

		status, body = jointCall(t, guarded, http.MethodPost, jointLearningPath(ids.bank, ""), "", limitCookie, "joint-limit-generate-0002")
		checks.eq(t, "second generation over the cap", status, http.StatusTooManyRequests)
		refused := jointDecode(t, "second generate", body)
		checks.eq(t, "second generation error", refused.Error, "practice_command_rate_limited")
		checks.ok(t, "refused generation queued nothing", len(refused.Data) == 0 || string(refused.Data) == "null", "body = %s", body)
		entitlement.assertClean(t)
		checks.ok(t, "the Core asked the entitlement service for the limited member", entitlement.checkedFor(ids.limitUser) >= 2,
			"checked = %d", entitlement.checkedFor(ids.limitUser))
	})
}
