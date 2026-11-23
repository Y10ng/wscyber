package webapi

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/chainreactors/cyber/core/operation"
	coretool "github.com/chainreactors/cyber/core/tool"
)

func TestToolValidationCollectsIndependentErrors(t *testing.T) {
	tests := []struct {
		name     string
		validate func(string) error
		input    string
		paths    []string
	}{
		{"report", validateReportDraftForErrorTest, `{"title":"", "overview":"", "items":[{"finding_id":"wf_1","description":"","reproduction":"","evidence_ids":["we_1","we_1"]}]}`, []string{"title", "overview", "items[0].description", "items[0].reproduction", "items[0].evidence_ids[1]"}},
		{"finding", validateFindingForErrorTest, `{"content":{"title":"", "description":"", "steps":[{"text":"", "evidence_indices":[-1,4]}], "evidence":[{"evidence_id":"", "relation":"bad"}]}}`, []string{"content.title", "content.description", "content.evidence[0].evidence_id", "content.evidence[0].relation", "content.steps[0].text", "content.steps[0].evidence_indices[0]", "content.steps[0].evidence_indices[1]"}},
		{"retest", validateRetestForErrorTest, `{"conclusion":"bad", "method":"", "summary":"", "evidence":[{"evidence_id":"", "relation":"bad"}]}`, []string{"conclusion", "method", "summary", "evidence[0].evidence_id", "evidence[0].relation"}},
		{"verification", validateVerificationForErrorTest, `{"verdict":"supported", "method":"", "summary":"", "key_steps":[{"title":"", "evidence_id":""}]}`, []string{"method", "summary", "impact", "key_steps[0].title", "key_steps[0].evidence_id"}},
		{"non-supported repro", validateVerificationForErrorTest, `{"verdict":"inconclusive", "method":"m", "summary":"s", "impact":"i", "retest_method":"r"}`, []string{"impact", "retest_method"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.validate(tt.input)
			requireToolValidationError(t, err, tt.paths...)
			if lines := strings.Split(err.Error(), "\n"); len(lines) < len(tt.paths) || len(lines) > maxToolValidationErrors {
				t.Fatalf("errors must be separate bounded lines: %q", err)
			}
		})
	}
	t.Run("ten-error budget including nested validators", func(t *testing.T) {
		args := validVerificationForErrorTest()
		for i := 0; i < 16; i++ {
			args.Evidence = append(args.Evidence, verificationEvidenceInput{EvidenceID: fmt.Sprint(i), Relation: "bad"})
		}
		args.Method, args.Summary = "", ""
		err := validateVerificationForErrorTest(mustToolArgumentsJSON(t, args))
		requireToolValidationError(t, err, "method", "summary", "evidence[7].relation")
		if len(strings.Split(err.Error(), "\n")) != 10 || strings.Contains(err.Error(), "evidence[8]") {
			t.Fatalf("budget: %v", err)
		}
	})
}

func TestReportPinnedErrorsUseIndexedPaths(t *testing.T) {
	detail := &ReportDetail{Items: []ReportItemDetail{
		{ReportItem: ReportItem{FindingID: "wf_0"}, Revision: &FindingDetail{}},
		{ReportItem: ReportItem{FindingID: "wf_1"}, Revision: &FindingDetail{}},
	}}
	for _, tt := range []struct {
		name  string
		items []reportDraftItemInput
		paths []string
	}{
		{"count", nil, []string{"items:", "期望 2"}},
		{"unknown and duplicate", []reportDraftItemInput{{FindingID: "missing"}, {FindingID: "wf_0"}, {FindingID: "wf_0"}}, []string{"items[0].finding_id", "items[2].finding_id"}},
		{"evidence", []reportDraftItemInput{{FindingID: "wf_0", EvidenceIDs: []string{"missing-a", "missing-b"}}, {FindingID: "wf_1"}}, []string{"items[0].evidence_ids[0]", "items[0].evidence_ids[1]", "固定修订"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requireToolValidationError(t, checkDraftAgainstReport(reportDraftPayload{Items: tt.items}, detail), tt.paths...)
		})
	}
}

func TestBoardToolValidationFieldPaths(t *testing.T) {
	tests := []struct {
		name  string
		tool  coretool.Tool
		input string
		paths []string
	}{
		{"read section", &boardReadTool{}, `{"section":"bad"}`, []string{"section", "summary/endpoints/facts/oracles"}},
		{"read filters", &boardReadTool{}, `{"section":"facts","status":"bad","category":"bad","limit":101}`, []string{"limit", "status", "category"}},
		{"read summary", &boardReadTool{}, `{"section":"summary","status":"active","category":"other","limit":2}`, []string{"status:", "category:", "limit:"}},
		{"map count", &boardMapTool{}, `{"endpoints":[]}`, []string{"endpoints", "1..50"}},
		{"map method url note", &boardMapTool{}, `{"endpoints":[{"method":"?","url":"https://example.test"},{"method":"GET","url":"relative","note":"\u0000"}]}`, []string{"endpoints[0].method", "endpoints[1].url", "endpoints[1].note"}},
		{"fact empty", &boardFactTool{}, `{}`, []string{"add", "retract", "至少"}},
		{"fact fields", &boardFactTool{}, `{"add":[{"kind":"bad","subject":"","statement":"","confidence":"bad","evidence_ids":["","same","same"]}],"retract":["","same","same"]}`, []string{"add[0].kind", "add[0].subject", "add[0].statement", "add[0].confidence", "add[0].evidence_ids[0]", "add[0].evidence_ids[2]", "retract[0]", "retract[2]"}},
		{"fact confirmed", &boardFactTool{}, `{"add":[{"kind":"other","subject":"s","statement":"s","confidence":"confirmed"}]}`, []string{"add[0].evidence_ids", "至少 1"}},
		{"oracle action", &boardOracleTool{}, `{"action":"bad"}`, []string{"action", "propose/claim/close/drop"}},
		{"oracle number", &boardOracleTool{}, `{"action":"claim","number":"bad","summary":"extra"}`, []string{"number", "O-001", "summary"}},
		{"oracle propose count", &boardOracleTool{}, `{"action":"propose","items":[]}`, []string{"items", "1..20"}},
		{"oracle proposal", &boardOracleTool{}, `{"action":"propose","items":[{"question":"","category":"bad","priority":"bad","endpoint_keys":["invalid"]}]}`, []string{"items[0].question", "items[0].category", "items[0].priority", "items[0].endpoint_keys[0]"}},
		{"oracle close", &boardOracleTool{}, `{"action":"close","number":"O-001","status":"holds","summary":""}`, []string{"summary", "finding_id", "evidence_ids"}},
		{"oracle close enum", &boardOracleTool{}, `{"action":"close","number":"O-001","status":"bad","summary":"s","finding_id":"wf_1"}`, []string{"status", "finding_id"}},
		{"oracle refuted", &boardOracleTool{}, `{"action":"close","number":"O-001","status":"refuted","summary":"s"}`, []string{"evidence_ids", "至少 1"}},
		{"oracle drop", &boardOracleTool{}, `{"action":"drop","number":"O-001","summary":""}`, []string{"summary", "非空"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.tool.Execute(t.Context(), tt.input)
			if err != nil || result == nil || !result.IsError {
				t.Fatalf("expected tool error: %v %+v", err, result)
			}
			text := coretool.ResultText(result)
			requireToolValidationError(t, errors.New(text), tt.paths...)
		})
	}
}

func TestAuthzCompareValidationFieldPaths(t *testing.T) {
	f := newAuthzFixture(t)
	tests := []struct {
		name     string
		evidence string
		variants []authzCompareVariantInput
		paths    []string
	}{
		{"required", "", nil, []string{"evidence_id", "variants"}},
		{"variant label", "missing", []authzCompareVariantInput{{Label: ""}, {Label: strings.Repeat("字", 41)}}, []string{"variants[0].label", "variants[1].label"}},
		{"headers", "missing", []authzCompareVariantInput{{Label: "a", Anonymous: true, SetHeaders: map[string]string{"Cookie": "x"}}, {Label: "b", SetHeaders: map[string]string{"Host": "x"}, RemoveHeaders: []string{"Host", "invalid header"}}}, []string{`variants[0].set_headers["Cookie"]`, `variants[1].set_headers["Host"]`, "variants[1].remove_headers[0]", "variants[1].remove_headers[1]"}},
		{"credential", "missing", []authzCompareVariantInput{{Label: "a", SetHeaders: map[string]string{"X-Test": "{{cred:missing.secret}}"}}}, []string{`variants[0].set_headers["X-Test"]`, "本任务关联资产"}},
		{"source", "missing", []authzCompareVariantInput{{Label: "a"}}, []string{"evidence_id", "不存在于本任务"}},
	}
	before := f.target.count()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, text := f.invokeCompare(t, f.inv, tt.evidence, tt.variants)
			if !result.IsError {
				t.Fatalf("accepted: %s", text)
			}
			requireToolValidationError(t, errors.New(text), append(tt.paths, "0发送")...)
		})
	}
	if f.target.count() != before {
		t.Fatal("invalid args sent HTTP requests")
	}
}

func TestSessionAndFindingEvidenceErrorsAreIndexed(t *testing.T) {
	f := newNotifyFixture(t)
	f.plainTask("t1")
	foreign := f.addEvidence("t1", "other-session", notifyFlow("GET", "https://api.example.test/items/1", "", `{}`))
	for _, label := range []string{"验证", "复测"} {
		t.Run(label, func(t *testing.T) {
			_, _, err := f.capability.store.sessionResultEvidence(t.Context(), "t1", "current-session", []verificationEvidenceInput{{EvidenceID: foreign.ID, Relation: "supporting"}, {EvidenceID: "missing", Relation: "limitation"}}, label)
			requireToolValidationError(t, err, "evidence[0].evidence_id", "evidence[1].evidence_id", "本"+label+"session")
		})
	}
	args := validFindingForErrorTest()
	args.Content.Evidence = []findingEvidenceInput{{EvidenceID: "missing-a", Relation: "supporting"}, {EvidenceID: "missing-b", Relation: "limitation"}}
	task, err := f.capability.store.Get(t.Context(), "t1")
	if err != nil {
		t.Fatal(err)
	}
	result, message := executeFinding(t, &findingTool{store: f.capability.store}, operation.Invocation{SessionID: task.InitialSessionID, TurnID: "t", CallID: "bad-evidence"}, mustToolArgumentsJSON(t, args))
	if result != nil {
		t.Fatal("accepted missing evidence")
	}
	requireToolValidationError(t, errors.New(message), "content.evidence[0].evidence_id", "content.evidence[1].evidence_id")
}

func TestStrictUnknownFieldsKeepNestedPaths(t *testing.T) {
	for _, tt := range []struct {
		name, input string
		target      any
		paths       []string
	}{
		{"report", `{"items":[{"extra":1},{"extra":2}]}`, &reportDraftPayload{}, []string{"items[0].extra", "items[1].extra"}},
		{"finding", `{"content":{"steps":[{"extra":1}]}}`, &findingToolArguments{}, []string{"content.steps[0].extra"}},
		{"oracle", `{"items":[{"extra":1}]}`, &boardOracleArguments{}, []string{"items[0].extra"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			requireToolValidationError(t, decodeStrictToolPayload(tt.input, tt.target, "测试"), tt.paths...)
		})
	}
}
