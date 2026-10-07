package tests

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	accountportfolio "henukit.dev/account-portfolio"
)

const quizCraftEntitlementSecret = "quizcraft-entitlement-private-secret-at-least-32bytes"

func newAccountPortfolioWithQuizCraft(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	clearAccountPortfolio(t, pool)
	handler, err := accountportfolio.New(accountportfolio.Config{
		Database: pool, ClientID: "portal-gateway", Keys: map[string]string{"account-key": serviceSecret},
		ConsoleClientID: "console-gateway", ConsoleKeys: map[string]string{"console-key": consoleServiceSecret},
		QuizCraftClientID: "quizcraft-learning", QuizCraftKeys: map[string]string{"quizcraft-key": quizCraftEntitlementSecret},
		PointCursorKey: pointCursorTestKey,
	})
	if err != nil {
		pool.Close()
		t.Fatal(err)
	}
	return httptest.NewServer(handler), pool
}

func sendQuizCraftEntitlement(t *testing.T, baseURL, actorID, route, nonce string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, baseURL+route, nil)
	if err != nil {
		t.Fatal(err)
	}
	ts := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	nonceSum := sha256.Sum256([]byte(nonce))
	encodedNonce := base64.RawURLEncoding.EncodeToString(nonceSum[:24])
	empty := sha256.Sum256(nil)
	canonical := strings.Join([]string{http.MethodGet, req.URL.RequestURI(), ts, encodedNonce, hex.EncodeToString(empty[:]), actorID}, "\n")
	mac := hmac.New(sha256.New, []byte(quizCraftEntitlementSecret))
	_, _ = mac.Write([]byte(canonical))
	req.SetBasicAuth("quizcraft-learning", quizCraftEntitlementSecret)
	req.Header.Set("X-Service-Id", "quizcraft-learning")
	req.Header.Set("X-Key-Id", "quizcraft-key")
	req.Header.Set("X-Actor-User-Id", actorID)
	req.Header.Set("X-Request-Id", "req_quizcraft_entitlement")
	req.Header.Set("X-Timestamp", ts)
	req.Header.Set("X-Nonce", encodedNonce)
	req.Header.Set("X-Signature", base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return response
}

func TestQuizCraftEntitlementIsReadOnlyCurrentAndCredentialScoped(t *testing.T) {
	server, pool := newAccountPortfolioWithQuizCraft(t)
	defer server.Close()
	defer pool.Close()
	const owner = "85858585-8585-4858-8858-858585858585"
	const operator = "94949494-9494-4949-8949-949494949494"
	const other = "73737373-7373-4737-8737-737373737373"
	route := "/api/v1/internal/quizcraft/entitlements/" + owner
	read := func(nonce string) (bool, int) {
		t.Helper()
		response := sendQuizCraftEntitlement(t, server.URL, owner, route, nonce)
		if response.StatusCode != http.StatusOK {
			t.Fatalf("entitlement status=%d %s", response.StatusCode, responseText(t, response))
		}
		var payload struct {
			Data struct {
				Lifetime bool `json:"lifetime"`
				Version  int  `json:"version"`
			} `json:"data"`
			RequestID string `json:"request_id"`
		}
		decodeResponse(t, response, &payload)
		if payload.RequestID == "" {
			t.Fatal("missing request ID")
		}
		return payload.Data.Lifetime, payload.Data.Version
	}
	if lifetime, version := read("quizcraft-uninitialized"); lifetime || version != 0 {
		t.Fatalf("uninitialized = %t/%d", lifetime, version)
	}
	var count int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM account_portfolio_memberships WHERE user_id=$1`, owner).Scan(&count); err != nil || count != 0 {
		t.Fatalf("read initialized private membership: %d %v", count, err)
	}
	denied := sendOwnerJSON(t, server.URL, http.MethodGet, owner, route, "portal-entitlement-deny", "", "")
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("portal credential accepted: %d %s", denied.StatusCode, responseText(t, denied))
	}
	_ = responseText(t, denied)
	denied = sendConsoleJSON(t, server.URL, http.MethodGet, operator, route, "console-entitlement-deny", "", "")
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("console credential accepted: %d %s", denied.StatusCode, responseText(t, denied))
	}
	_ = responseText(t, denied)
	denied = sendQuizCraftEntitlement(t, server.URL, other, route, "quizcraft-mismatch")
	if denied.StatusCode != http.StatusForbidden {
		t.Fatalf("actor mismatch accepted: %d %s", denied.StatusCode, responseText(t, denied))
	}
	_ = responseText(t, denied)
	for _, privateRoute := range []string{"/api/v1/account/summary", "/api/v1/console/memberships/" + owner} {
		denied = sendQuizCraftEntitlement(t, server.URL, owner, privateRoute, "quizcraft-cross-"+privateRoute)
		if denied.StatusCode != http.StatusForbidden {
			t.Fatalf("QuizCraft credential read %s: %d %s", privateRoute, denied.StatusCode, responseText(t, denied))
		}
		_ = responseText(t, denied)
	}
	unmatched := sendQuizCraftEntitlement(t, server.URL, owner, route+"/extra", "quizcraft-unmatched")
	if unmatched.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown internal path leaked: %d %s", unmatched.StatusCode, responseText(t, unmatched))
	}
	_ = responseText(t, unmatched)
	first := sendQuizCraftEntitlement(t, server.URL, owner, route, "quizcraft-replay")
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first read: %d %s", first.StatusCode, responseText(t, first))
	}
	_ = responseText(t, first)
	replay := sendQuizCraftEntitlement(t, server.URL, owner, route, "quizcraft-replay")
	if replay.StatusCode != http.StatusConflict {
		t.Fatalf("replay status: %d %s", replay.StatusCode, responseText(t, replay))
	}
	_ = responseText(t, replay)
	summary := sendOwnerJSON(t, server.URL, http.MethodGet, owner, "/api/v1/account/summary", "owner-entitlement-seed", "", "")
	if summary.StatusCode != http.StatusOK {
		t.Fatalf("seed owner: %d %s", summary.StatusCode, responseText(t, summary))
	}
	_ = responseText(t, summary)
	granted := sendConsoleJSON(t, server.URL, http.MethodPost, operator, "/api/v1/console/memberships/"+owner+"/grants", "grant-entitlement", "idem_quizcraft_grant", `{"reason":"Verified study service test grant.","expected_version":1}`)
	if granted.StatusCode != http.StatusOK {
		t.Fatalf("grant: %d %s", granted.StatusCode, responseText(t, granted))
	}
	_ = responseText(t, granted)
	if lifetime, version := read("quizcraft-granted"); !lifetime || version != 2 {
		t.Fatalf("grant was not live: %t/%d", lifetime, version)
	}
	revoked := sendConsoleJSON(t, server.URL, http.MethodPost, operator, "/api/v1/console/memberships/"+owner+"/revocations", "revoke-entitlement", "idem_quizcraft_revoke", `{"reason":"Verified study service test revocation.","expected_version":2}`)
	if revoked.StatusCode != http.StatusOK {
		t.Fatalf("revoke: %d %s", revoked.StatusCode, responseText(t, revoked))
	}
	_ = responseText(t, revoked)
	if lifetime, version := read("quizcraft-revoked"); lifetime || version != 3 {
		t.Fatalf("revocation was not live: %t/%d", lifetime, version)
	}
	// The response is intentionally minimal: no points, payments or student data.
	raw := sendQuizCraftEntitlement(t, server.URL, owner, route, "quizcraft-minimal")
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(readBody(t, raw), &envelope); err != nil {
		t.Fatal(err)
	}
	var data map[string]any
	if err := json.Unmarshal(envelope["data"], &data); err != nil {
		t.Fatal(err)
	}
	if len(data) != 2 {
		t.Fatalf("unexpected account data exported: %+v", data)
	}
}

func TestQuizCraftEntitlementRequiresIndependentSecrets(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), testDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	base := accountportfolio.Config{Database: pool, ClientID: "portal-gateway", Keys: map[string]string{"account-key": serviceSecret}, PointCursorKey: pointCursorTestKey}
	base.QuizCraftClientID = "quizcraft-learning"
	if _, err := accountportfolio.New(base); err == nil {
		t.Fatal("partial QuizCraft credential accepted")
	}
	base.QuizCraftKeys = map[string]string{"key": serviceSecret}
	if _, err := accountportfolio.New(base); err == nil {
		t.Fatal("shared owner secret accepted")
	}
	base.QuizCraftKeys = map[string]string{"key": quizCraftEntitlementSecret}
	base.QuizCraftClientID = "portal-gateway"
	if _, err := accountportfolio.New(base); err == nil {
		t.Fatal("shared owner client ID accepted")
	}
}
