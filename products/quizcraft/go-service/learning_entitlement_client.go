package quizcraft

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

const learningEntitlementPath = "/api/v1/internal/quizcraft/entitlements/"

var ErrLearningEntitlementUnavailable = errors.New("learning membership entitlement is unavailable")

type LearningEntitlementClientConfig struct {
	BaseURL    string
	ClientID   string
	KeyID      string
	Secret     string
	HTTPClient *http.Client
}

type LearningEntitlementClient struct {
	baseURL, clientID, keyID, secret string
	httpClient                       *http.Client
}

// The credentials are separate from Portal/Console service credentials. An
// HTTP origin is allowed only for loopback or the private Account Portfolio
// service name; a public origin must be HTTPS. Never accept a browser URL.
func NewLearningEntitlementClient(config LearningEntitlementClientConfig) (*LearningEntitlementClient, error) {
	raw := strings.TrimSpace(config.BaseURL)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "https" && parsed.Scheme != "http") || strings.TrimSpace(config.ClientID) == "" || strings.TrimSpace(config.KeyID) == "" || len(config.Secret) < 32 {
		return nil, ErrLearningEntitlementUnavailable
	}
	hostname := parsed.Hostname()
	if parsed.Scheme == "http" {
		address := net.ParseIP(hostname)
		if hostname != "localhost" && hostname != "account-portfolio" && !strings.HasSuffix(hostname, ".internal") && (address == nil || !address.IsLoopback()) {
			return nil, ErrLearningEntitlementUnavailable
		}
	}
	client := http.Client{Timeout: 3 * time.Second}
	if config.HTTPClient != nil {
		client = *config.HTTPClient
	}
	if client.Timeout <= 0 || client.Timeout > 3*time.Second {
		client.Timeout = 3 * time.Second
	}
	// Go may forward Authorization when redirecting to related origins.
	// Returning the 3xx to the caller prevents any signed credential leakage.
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &LearningEntitlementClient{baseURL: raw, clientID: config.ClientID, keyID: config.KeyID, secret: config.Secret, httpClient: &client}, nil
}

// CheckLifetime is deliberately uncached: generation, model work, publication
// and report reads must see a current owner decision. Every failure denies.
func (client *LearningEntitlementClient) CheckLifetime(ctx context.Context, userID uuid.UUID) (bool, error) {
	if client == nil || userID == uuid.Nil {
		return false, ErrLearningEntitlementUnavailable
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+learningEntitlementPath+userID.String(), nil)
	if err != nil {
		return false, ErrLearningEntitlementUnavailable
	}
	nonceBytes := make([]byte, 24)
	if _, err := rand.Read(nonceBytes); err != nil {
		return false, ErrLearningEntitlementUnavailable
	}
	nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)
	timestamp := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	digest := sha256.Sum256(nil)
	canonical := strings.Join([]string{http.MethodGet, request.URL.RequestURI(), timestamp, nonce, hex.EncodeToString(digest[:]), userID.String()}, "\n")
	mac := hmac.New(sha256.New, []byte(client.secret))
	_, _ = mac.Write([]byte(canonical))
	request.SetBasicAuth(client.clientID, client.secret)
	request.Header.Set("X-Service-Id", client.clientID)
	request.Header.Set("X-Key-Id", client.keyID)
	request.Header.Set("X-Actor-User-Id", userID.String())
	request.Header.Set("X-Timestamp", timestamp)
	request.Header.Set("X-Nonce", nonce)
	request.Header.Set("X-Signature", base64.RawURLEncoding.EncodeToString(mac.Sum(nil)))
	request.Header.Set("X-Request-Id", "req_learning_"+uuid.NewString())
	response, err := client.httpClient.Do(request)
	if err != nil {
		return false, ErrLearningEntitlementUnavailable
	}
	defer response.Body.Close()
	contentType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if response.StatusCode != http.StatusOK || err != nil || contentType != "application/json" {
		return false, ErrLearningEntitlementUnavailable
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return false, ErrLearningEntitlementUnavailable
	}
	var envelope struct {
		Data struct {
			Lifetime *bool `json:"lifetime"`
			Version  *int  `json:"version"`
		} `json:"data"`
		RequestID string `json:"request_id"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&envelope) != nil || ensureJSONEOF(decoder) != nil || envelope.Data.Lifetime == nil || envelope.Data.Version == nil || *envelope.Data.Version < 0 ||
		(*envelope.Data.Lifetime && *envelope.Data.Version == 0) || envelope.RequestID == "" {
		return false, ErrLearningEntitlementUnavailable
	}
	return *envelope.Data.Lifetime, nil
}
