package practice

import (
	"strings"
	"testing"
)

const (
	commandLearningReportBankID = "10ca9b18-c303-4b7a-ab14-1241e41b665a"
	commandLearningReportTaskID = "c1d2e3f4-a5b6-4c7d-8e9f-0a1b2c3d4e5f"
)

const validLearningReportPreferencesCommand = `{"request_id":"req_core_preferences","data":{"enabled":true,"interval_days":7,"goal":"exam_review","chapter_ids":["ch01"],"external_analysis_consent":true,"bank_id":"` + commandLearningReportBankID + `","revision":3,"next_due_at":"2026-10-09T00:00:00Z","updated_at":"2026-10-02T00:00:00Z"}}`

const validLearningReportTaskCommand = `{"request_id":"req_core_task","data":{"task_id":"` + commandLearningReportTaskID + `","bank_id":"` + commandLearningReportBankID + `","status":"queued","created_at":"2026-10-02T00:00:00Z"}}`

const validLearningReportClearCommand = `{"request_id":"req_core_clear","data":{"cleared":true,"revision":4}}`

func TestLearningReportWriteEnvelopesAcceptOnlyTheModelledShape(t *testing.T) {
	for _, test := range []struct {
		name     string
		validate func([]byte) error
		raw      string
	}{
		{name: "saved preferences", validate: validateLearningReportPreferencesCommandEnvelope, raw: validLearningReportPreferencesCommand},
		{name: "queued task", validate: validateLearningReportTaskCommandEnvelope(commandLearningReportBankID), raw: validLearningReportTaskCommand},
		{name: "cleared derived reports", validate: validateLearningReportClearCommandEnvelope, raw: validLearningReportClearCommand},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.validate([]byte(test.raw)); err != nil {
				t.Fatalf("rejected a valid Core write response: %v", err)
			}
		})
	}
}

func TestLearningReportWriteEnvelopesRejectUnmodelledOrContradictoryData(t *testing.T) {
	preferences := func(mutate func(string) string) func([]byte) error {
		return func(raw []byte) error {
			return validateLearningReportPreferencesCommandEnvelope([]byte(mutate(string(raw))))
		}
	}
	task := func(mutate func(string) string) func([]byte) error {
		return func(raw []byte) error {
			return validateLearningReportTaskCommandEnvelope(commandLearningReportBankID)([]byte(mutate(string(raw))))
		}
	}
	for _, test := range []struct {
		name     string
		validate func([]byte) error
		raw      string
	}{
		{
			name:     "unmodelled top level member",
			validate: validateLearningReportPreferencesCommandEnvelope,
			raw:      `{"request_id":"req_core_preferences","internal_note":"must not reach a browser","data":{"enabled":true,"interval_days":7,"goal":"exam_review","chapter_ids":[],"external_analysis_consent":true,"bank_id":"` + commandLearningReportBankID + `","revision":3}}`,
		},
		{
			name:     "unmodelled data member",
			validate: validateLearningReportPreferencesCommandEnvelope,
			raw:      `{"request_id":"req_core_preferences","data":{"enabled":true,"interval_days":7,"goal":"exam_review","chapter_ids":[],"external_analysis_consent":true,"bank_id":"` + commandLearningReportBankID + `","revision":3,"user_id":"spoofed"}}`,
		},
		{
			name:     "period outside the contract",
			validate: preferences(func(raw string) string { return replaceOnce(raw, `"interval_days":7`, `"interval_days":31`) }),
			raw:      validLearningReportPreferencesCommand,
		},
		{
			name:     "unknown goal",
			validate: preferences(func(raw string) string { return replaceOnce(raw, `"goal":"exam_review"`, `"goal":"cram"`) }),
			raw:      validLearningReportPreferencesCommand,
		},
		{
			name:     "task status outside the contract",
			validate: task(func(raw string) string { return replaceOnce(raw, `"status":"queued"`, `"status":"done"`) }),
			raw:      validLearningReportTaskCommand,
		},
		{
			name: "task for another bank",
			validate: task(func(raw string) string {
				return replaceOnce(raw, commandLearningReportBankID, "0e5a7b9c-1d2e-4f3a-8b9c-0d1e2f3a4b5c")
			}),
			raw: validLearningReportTaskCommand,
		},
		{
			name:     "clear reported as not cleared",
			validate: validateLearningReportClearCommandEnvelope,
			raw:      replaceOnce(validLearningReportClearCommand, `"cleared":true`, `"cleared":false`),
		},
		{
			name:     "clear without a revision",
			validate: validateLearningReportClearCommandEnvelope,
			raw:      `{"request_id":"req_core_clear","data":{"cleared":true}}`,
		},
		{
			name:     "clear with an unmodelled member",
			validate: validateLearningReportClearCommandEnvelope,
			raw:      `{"request_id":"req_core_clear","data":{"cleared":true,"revision":4,"report_ids":["x"]}}`,
		},
		{
			name:     "missing request id",
			validate: validateLearningReportClearCommandEnvelope,
			raw:      `{"data":{"cleared":true,"revision":4}}`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.validate([]byte(test.raw)); err == nil {
				t.Fatalf("accepted an unmodelled or contradictory Core write response: %s", test.raw)
			}
		})
	}
}

func replaceOnce(value, old, new string) string {
	return strings.Replace(value, old, new, 1)
}
