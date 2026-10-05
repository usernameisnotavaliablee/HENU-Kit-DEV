package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
)

var buildReleaseSHA = "development"

func main() {
	databaseURL := requiredEnv("DATABASE_URL")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	fail(err)
	defer pool.Close()
	fail(pool.Ping(ctx))
	fail(quizcraft.RequireQuizcraftV2Target(ctx, pool))
	if len(os.Args) == 2 && os.Args[1] == "verify-practice" {
		report, err := quizcraft.VerifyPracticeFlow(ctx, pool)
		fail(err)
		fmt.Printf("practice flow verified for bank %s question %s\n", report.BankID, report.QuestionID)
		return
	}
	if len(os.Args) != 1 {
		fail(fmt.Errorf("unknown QuizCraft command"))
	}
	authSecret := requiredEnv("QUIZCRAFT_AUTH_HMAC_SECRET")
	entitlement, err := learningEntitlementFromEnv()
	fail(err)
	worker, err := learningWorkerFromEnv()
	fail(err)
	if worker != nil && entitlement == nil {
		fail(errors.New("QuizCraft learning report worker requires the signed entitlement client"))
	}
	address := os.Getenv("QUIZCRAFT_HTTP_ADDR")
	if address == "" {
		address = ":8080"
	}
	summaryKeys := map[string]string{}
	if keyID := os.Getenv("QUIZCRAFT_SUMMARY_KEY_ID"); keyID != "" {
		summaryKeys[keyID] = os.Getenv("QUIZCRAFT_SUMMARY_CLIENT_SECRET")
	}
	catalogKeys := map[string]string{}
	if keyID := os.Getenv("QUIZCRAFT_PORTAL_CATALOG_KEY_ID"); keyID != "" {
		catalogKeys[keyID] = os.Getenv("QUIZCRAFT_PORTAL_CATALOG_CLIENT_SECRET")
	}
	portalCommandKeys := map[string]string{}
	if keyID := os.Getenv("QUIZCRAFT_PORTAL_COMMAND_KEY_ID"); keyID != "" {
		portalCommandKeys[keyID] = os.Getenv("QUIZCRAFT_PORTAL_COMMAND_CLIENT_SECRET")
	}
	handler, err := quizcraft.NewPracticeHTTP(quizcraft.PracticeHTTPConfig{
		Database:              pool,
		AuthHMACSecret:        []byte(authSecret),
		LearningEntitlement:   entitlement,
		LegacyBaseURL:         os.Getenv("QUIZCRAFT_LEGACY_BASE_URL"),
		LegacyCompareSecret:   os.Getenv("QUIZCRAFT_LEGACY_COMPARE_SECRET"),
		SummaryClientID:       os.Getenv("QUIZCRAFT_SUMMARY_CLIENT_ID"),
		SummaryKeys:           summaryKeys,
		CatalogClientID:       os.Getenv("QUIZCRAFT_PORTAL_CATALOG_CLIENT_ID"),
		CatalogKeys:           catalogKeys,
		PortalCommandClientID: os.Getenv("QUIZCRAFT_PORTAL_COMMAND_CLIENT_ID"),
		PortalCommandKeys:     portalCommandKeys,
		PortalCommandsEnabled: os.Getenv("QUIZCRAFT_PORTAL_COMMANDS_ENABLED") == "1",
		PlatformCoreURL:       os.Getenv("PLATFORM_CORE_URL"),
		PlatformClientID:      os.Getenv("QUIZCRAFT_PLATFORM_CLIENT_ID"),
		PlatformClientSecret:  os.Getenv("QUIZCRAFT_PLATFORM_CLIENT_SECRET"),
		PlatformKeyID:         os.Getenv("QUIZCRAFT_PLATFORM_KEY_ID"),
		PublicURL:             os.Getenv("QUIZCRAFT_PUBLIC_URL"),
		SessionEncryptionKey:  []byte(os.Getenv("QUIZCRAFT_SESSION_ENCRYPTION_KEY")),
		InboxExchangeToken:    os.Getenv("QUIZCRAFT_INBOX_EXCHANGE_TOKEN"),
		WritesDisabled:        requiredEnv("QUIZCRAFT_WRITES_ENABLED") != "1",
		ReleaseSHA:            buildReleaseSHA,
		CutoverEvidenceSecret: []byte(requiredEnv("QUIZCRAFT_CUTOVER_EVIDENCE_SECRET")),
	})
	fail(err)
	server := &http.Server{Addr: address, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	// The report worker is opt-in and only starts with a validated provider and
	// the signed entitlement client; every other deployment stays dark.
	if worker != nil {
		service, err := quizcraft.New(quizcraft.Config{Database: pool})
		fail(err)
		workerContext, stopWorker := context.WithCancel(context.Background())
		defer stopWorker()
		log.Printf("QuizCraft learning report worker started (poll %s, lease %s)", worker.Poll, worker.Lease)
		go func() {
			if err := quizcraft.RunLearningWorker(workerContext, worker.Poll, func(stepContext context.Context) (bool, error) {
				processed, err := service.ProcessNextLearningReport(stepContext, entitlement, worker.Provider, worker.Versions, worker.Lease)
				if err != nil {
					// The job row already carries the retry state; this line is
					// the only operator-visible trace of a failing queue.
					log.Printf("QuizCraft learning report step failed: %v", err)
				}
				return processed, err
			}); err != nil {
				log.Printf("QuizCraft learning report worker stopped: %v", err)
			}
		}()
	}
	log.Printf("QuizCraft Practice shadow service listening on %s", address)
	err = server.ListenAndServe()
	if !errors.Is(err, http.ErrServerClosed) {
		fail(err)
	}
}

func requiredEnv(name string) string {
	value := os.Getenv(name)
	if value == "" {
		fail(fmt.Errorf("%s is required", name))
	}
	return value
}

func fail(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
