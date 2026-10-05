package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	quizcraft "henukit.dev/quizcraft"
)

// learninghealth reports whether learning feedback is running and whether it is
// failing. It only reads, so operators can run it against production, and it can
// be used as a cron gate through --fail-on-alert.
type options struct {
	json          bool
	queuedBehind  time.Duration
	failureBudget int
	failOnAlert   bool
}

func main() {
	if err := run(context.Background(), os.Args[1:]); errors.Is(err, flag.ErrHelp) {
		return
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "QuizCraft learning feedback health check failed")
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("learninghealth", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	value := options{}
	flags.BoolVar(&value.json, "json", false, "print the health read as JSON")
	flags.DurationVar(&value.queuedBehind, "queued-behind", 30*time.Minute, "alert when the oldest queued request is older than this")
	flags.IntVar(&value.failureBudget, "failure-budget", 0, "how many failures in the last 24h are tolerated")
	flags.BoolVar(&value.failOnAlert, "fail-on-alert", false, "exit non-zero when any alert is raised")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if value.queuedBehind <= 0 || value.queuedBehind > 24*time.Hour || value.failureBudget < 0 || value.failureBudget > 100000 {
		return errors.New("queued-behind must be between 0 and 24h and failure-budget between 0 and 100000")
	}
	databaseURL := os.Getenv("QUIZCRAFT_V2_DATABASE_URL")
	if err := quizcraft.RequireQuizcraftV2DatabaseURL(databaseURL); err != nil {
		return err
	}
	database, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer database.Close()
	if err := quizcraft.RequireQuizcraftV2Target(ctx, database); err != nil {
		return err
	}
	now := time.Now()
	health, err := quizcraft.ReadLearningFeedbackHealth(ctx, database, now)
	if err != nil {
		return err
	}
	alerts := quizcraft.HealthAlerts(health, value.queuedBehind, value.failureBudget)
	if value.json {
		output, err := json.MarshalIndent(struct {
			quizcraft.LearningFeedbackHealth
			Alerts []string `json:"alerts"`
		}{health, alerts}, "", "  ")
		if err != nil {
			return err
		}
		fmt.Println(string(output))
	} else {
		printHealth(health, alerts)
	}
	if value.failOnAlert && len(alerts) > 0 {
		return fmt.Errorf("%d learning feedback alert(s)", len(alerts))
	}
	return nil
}

func printHealth(health quizcraft.LearningFeedbackHealth, alerts []string) {
	fmt.Printf("learning feedback health at %s\n", health.GeneratedAt.Format(time.RFC3339))
	fmt.Printf("courses: enabled=%d consented_member_courses=%d\n", health.EnabledCourses, health.ConsentedCourses)
	fmt.Printf("jobs by status: %s\n", formatCounts(health.JobsByStatus))
	if health.QueuedOldestAge != nil {
		fmt.Printf("oldest queued request: %s\n", health.QueuedOldestAge.Round(time.Second))
	} else {
		fmt.Println("oldest queued request: none")
	}
	fmt.Printf("stale worker leases: %d\n", health.StaleLeases)
	fmt.Printf("failures in last 24h: %s\n", formatCounts(health.Failures24h))
	fmt.Printf("reports by status: %s\n", formatCounts(health.ReportsByStatus))
	if health.LatestReportAt != nil {
		fmt.Printf("latest report record: %s\n", health.LatestReportAt.UTC().Format(time.RFC3339))
	} else {
		fmt.Println("latest report record: none")
	}
	if len(alerts) == 0 {
		fmt.Println("alerts: none")
		return
	}
	for _, alert := range alerts {
		fmt.Printf("ALERT: %s\n", alert)
	}
}

func formatCounts(counts map[string]int) string {
	if len(counts) == 0 {
		return "none"
	}
	statuses := make([]string, 0, len(counts))
	for status := range counts {
		statuses = append(statuses, status)
	}
	sort.Strings(statuses)
	parts := make([]string, 0, len(statuses))
	for _, status := range statuses {
		parts = append(parts, fmt.Sprintf("%s=%d", status, counts[status]))
	}
	return fmt.Sprint(parts)
}
