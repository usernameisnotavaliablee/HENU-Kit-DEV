package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"henukit.dev/quizcraft/internal/contract"
)

// reportReadOperations are the learning operations served by the actor-bound
// catalog read middleware rather than the practice command credentials.
var reportReadOperations = map[string]bool{
	"getPortalLearningReportPreferences": true,
	"getPortalLatestLearningReport":      true,
	"getPortalLearningReportTask":        true,
}

func TestLearningReportContractRequiresSignedOwner(t *testing.T) {
	if contract.LearningReportPracticeSessionRoute != "/api/v1/portal/practice/banks/{bank_id}/learning-reports/results/{report_id}/practice-sessions" {
		t.Fatal("report result route must not overlap tasks/{task_id}")
	}
	doc, err := openapi3.NewLoader().LoadFromFile("../../../../packages/api-contracts/openapi/quizcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}
	routes := map[string]string{
		"getPortalLearningReportPreferences":        contract.GetLearningReportPreferencesRoute,
		"updatePortalLearningReportPreferences":     contract.UpdateLearningReportPreferencesRoute,
		"requestPortalLearningReport":               contract.RequestLearningReportRoute,
		"clearPortalLearningReports":                contract.ClearLearningReportsRoute,
		"getPortalLatestLearningReport":             contract.LatestLearningReportRoute,
		"getPortalLearningReportTask":               contract.LearningReportTaskRoute,
		"createPortalLearningReportPracticeSession": contract.LearningReportPracticeSessionRoute,
	}
	seen := map[string]bool{}
	for path, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			if len(op.Tags) != 1 || op.Tags[0] != "LearningReports" {
				continue
			}
			if routes[op.OperationID] != path {
				t.Fatalf("unmapped learning operation %s at %s", op.OperationID, path)
			}
			seen[op.OperationID] = true
			if op.Security == nil || len(*op.Security) != 1 {
				t.Fatalf("%s: explicit combined service auth required", op.OperationID)
			}
			security := (*op.Security)[0]
			// The three report GETs are served by the actor-bound catalog read
			// middleware (authenticatePortalPersonalStats), so they must document
			// that six-part scheme and its service-replay conflict. Only the write
			// operations use the practice command credential pair.
			wantSchemes := []string{"portalPracticeBasic", "portalPracticeSignature"}
			wantConflict := "Conflict"
			if reportReadOperations[op.OperationID] {
				wantSchemes = []string{
					"portalCatalogBasic", "portalCatalogSignature", "portalCatalogPermission",
					"portalCatalogScope", "portalCatalogProduct", "portalCatalogActor",
				}
				wantConflict = "ServiceReplay"
			}
			for _, scheme := range wantSchemes {
				if _, ok := security[scheme]; !ok {
					t.Fatalf("%s: security scheme %s missing", op.OperationID, scheme)
				}
			}
			if len(security) != len(wantSchemes) {
				t.Fatalf("%s: security schemes = %v, want %v", op.OperationID, security, wantSchemes)
			}
			// Check the raw response-component ref: kin-openapi resolves the
			// chain, so the resolved schema cannot tell replay from conflict.
			conflict, ok := op.Responses.Map()["409"]
			if !ok || conflict == nil || conflict.Value == nil {
				t.Fatalf("%s: conflict response missing", op.OperationID)
			}
			if !strings.HasSuffix(conflict.Ref, "/"+wantConflict) {
				t.Fatalf("%s: 409 ref = %q, want %s", op.OperationID, conflict.Ref, wantConflict)
			}
			actor := false
			for _, param := range op.Parameters {
				if p := param.Value; p.In == "header" && p.Name == "X-Actor-User-Id" && p.Required {
					actor = true
				}
			}
			if !actor {
				t.Fatalf("%s: required owner header missing", op.OperationID)
			}
			if op.Responses.Value("503") == nil {
				t.Fatalf("%s: dependency failure missing", op.OperationID)
			}
		}
	}
	if len(seen) != len(routes) {
		t.Fatalf("got %d learning operations, want %d", len(seen), len(routes))
	}
}

func TestLearningReportPreferenceContractBounds(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../../../packages/api-contracts/openapi/quizcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	schema := doc.Components.Schemas["LearningReportPreferencesUpdate"].Value
	payload := map[string]any{"enabled": false, "interval_days": float64(7), "goal": "follow_course", "chapter_ids": []any{}, "external_analysis_consent": false}
	if err := schema.VisitJSON(payload); err != nil {
		t.Fatal(err)
	}
	for _, interval := range []float64{0, 31, 1.5} {
		payload["interval_days"] = interval
		if schema.VisitJSON(payload) == nil {
			t.Fatalf("invalid period %v accepted", interval)
		}
	}
	payload["interval_days"] = float64(7)
	payload["user_id"] = "spoofed-owner"
	if schema.VisitJSON(payload) == nil {
		t.Fatal("JSON actor injection accepted")
	}
	delete(payload, "user_id")
	delete(payload, "external_analysis_consent")
	if schema.VisitJSON(payload) == nil {
		t.Fatal("missing consent accepted")
	}
	findingLimit := doc.Components.Schemas["LearningReport"].Value.Properties["findings"].Value.MaxItems
	if findingLimit == nil || *findingLimit != 3 {
		t.Fatal("report must highlight at most three findings")
	}
}
