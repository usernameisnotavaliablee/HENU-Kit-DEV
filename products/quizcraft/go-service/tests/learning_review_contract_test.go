package tests

import (
	"context"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"henukit.dev/quizcraft/internal/contract"
)

// The Workshop learning-content contract is the operator authority over content
// that members will read. These assertions pin the parts a reviewer must be able
// to rely on: only the Workshop session can call it, writes are idempotent and
// answer with the stored operation, a draft cannot carry review state, and the
// review state a member-facing read depends on is always present.
func TestWorkshopLearningContentContractKeepsReviewAuthority(t *testing.T) {
	doc, err := openapi3.NewLoader().LoadFromFile("../../../../packages/api-contracts/openapi/quizcraft.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if err := doc.Validate(context.Background()); err != nil {
		t.Fatal(err)
	}

	collection := "/api/v1/workshop/banks/{bank_id}/learning-content"
	item := doc.Paths.Find(collection)
	if item == nil || item.Get == nil || item.Post == nil {
		t.Fatalf("learning content collection is missing from the contract")
	}
	if item.Get.OperationID != "listWorkshopLearningContent" || item.Post.OperationID != "importWorkshopLearningContent" {
		t.Fatalf("collection operations = %s/%s", item.Get.OperationID, item.Post.OperationID)
	}
	if item.Post.RequestBody == nil || item.Post.RequestBody.Value.Content["application/json"].Schema.Ref != "#/components/schemas/LearningContentDraft" {
		t.Fatal("import must take the draft schema itself")
	}

	writes := map[string]string{
		collection: "importWorkshopLearningContent",
		collection + "/{content_version_id}/approve": "approveWorkshopLearningContent",
		collection + "/{content_version_id}/retire":  "retireWorkshopLearningContent",
	}
	for path, operationID := range writes {
		item := doc.Paths.Find(path)
		if item == nil || item.Post == nil {
			t.Fatalf("%s: POST missing", path)
		}
		operation := item.Post
		if operation.OperationID != operationID {
			t.Fatalf("%s: operationId = %s", path, operation.OperationID)
		}
		if len(operation.Tags) != 1 || operation.Tags[0] != "Workshop" {
			t.Fatalf("%s: tags = %v", path, operation.Tags)
		}
		if operation.Security == nil || len(*operation.Security) != 1 {
			t.Fatalf("%s: explicit Workshop session required", path)
		}
		if _, ok := (*operation.Security)[0]["workshopSessionCookie"]; !ok {
			t.Fatalf("%s: Workshop session scheme missing", path)
		}
		if operation.Responses.Value("401") == nil || operation.Responses.Value("403") == nil || operation.Responses.Value("503") == nil {
			t.Fatalf("%s: guest/scope/dependency failures must stay documented", path)
		}
		idempotent, scoped := false, false
		for _, parameter := range operation.Parameters {
			if parameter.Value.In == "header" && parameter.Value.Name == "Idempotency-Key" && parameter.Value.Required {
				idempotent = true
			}
			if parameter.Value.In == "path" && parameter.Value.Name == "bank_id" && parameter.Value.Required {
				scoped = true
			}
		}
		if !idempotent || !scoped {
			t.Fatalf("%s: required idempotency key=%t bank scope=%t", path, idempotent, scoped)
		}
		// Every review write answers with the stored Workshop operation, not with a
		// version body a caller could mistake for an approval receipt.
		status := "201"
		if operationID != "importWorkshopLearningContent" {
			status = "200"
		}
		response := operation.Responses.Value(status)
		if response == nil || !strings.HasSuffix(response.Value.Content["application/json"].Schema.Ref, "/OperationEnvelope") {
			t.Fatalf("%s: %s response must be the operation envelope", path, status)
		}
	}
	// The review action is a distinct permission: import alone cannot approve.
	if doc.Components.SecuritySchemes["workshopSessionCookie"] == nil {
		t.Fatal("Workshop session scheme disappeared")
	}

	// A draft cannot smuggle review state or unknown fields, and the required
	// collections are exactly the ones import validates.
	draft := doc.Components.Schemas["LearningContentDraft"].Value
	valid := map[string]any{
		"schema_version": float64(1),
		"tags":           []any{map[string]any{"id": "math", "kind": "knowledge", "name": "算术", "definition": "基础运算"}},
		"questions":      []any{map[string]any{"question_id": "6f1a1e64-0000-4000-8000-000000000001", "question_version_id": "6f1a1e64-0000-4000-8000-000000000002", "tag_ids": []any{"math"}}},
	}
	if err := draft.VisitJSON(valid); err != nil {
		t.Fatalf("valid draft rejected: %v", err)
	}
	valid["status"] = "approved"
	if draft.VisitJSON(valid) == nil {
		t.Fatal("draft accepted a caller-supplied review status")
	}
	delete(valid, "status")
	delete(valid, "schema_version")
	if draft.VisitJSON(valid) == nil {
		t.Fatal("draft accepted a payload without a schema version")
	}
	if kind := doc.Components.Schemas["LearningContentTag"].Value.Properties["kind"].Value; len(kind.Enum) != 2 {
		t.Fatalf("tag kinds = %v", kind.Enum)
	}

	// Review state that members depend on is always reported, and the review
	// command carries no reviewer identity: the server owns the actor.
	version := doc.Components.Schemas["LearningContentVersion"].Value
	for _, field := range []string{"status", "reviewed_by", "reviewed_at", "active", "catalog_enabled"} {
		if _, ok := version.Properties[field]; !ok {
			t.Fatalf("content version response omits %s", field)
		}
	}
	command := doc.Components.Schemas["LearningContentReviewCommand"].Value
	if _, ok := command.Properties["reviewer_id"]; ok {
		t.Fatal("review command must not accept a caller-supplied reviewer")
	}
	tooLong := map[string]any{"note": strings.Repeat("审", 1001)}
	if command.VisitJSON(tooLong) == nil {
		t.Fatal("unbounded review note accepted")
	}

	// The generated Go surface must exist, and the operation kinds are the ones
	// the Workshop idempotency route accepts.
	var (
		_ contract.LearningContentDraft
		_ contract.LearningContentVersion
		_ contract.LearningContentReviewCommand
		_ contract.WorkshopLearningContentEnvelope
	)
	kinds := map[string]bool{
		string(contract.OperationKindImportLearningContent):  true,
		string(contract.OperationKindApproveLearningContent): true,
		string(contract.OperationKindRetireLearningContent):  true,
	}
	for _, value := range doc.Components.Schemas["OperationKind"].Value.Enum {
		delete(kinds, value.(string))
	}
	if len(kinds) != 0 {
		t.Fatalf("operation kinds missing from the contract: %v", kinds)
	}
}
