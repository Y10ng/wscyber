package webapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func mustToolArgumentsJSON(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func requireToolValidationError(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("invalid tool arguments accepted")
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Fatalf("error %q does not contain %q", err, part)
		}
	}
}

func validReportDraftForErrorTest() reportDraftPayload {
	items := make([]reportDraftItemInput, 3)
	for index := range items {
		items[index] = reportDraftItemInput{
			FindingID:    fmt.Sprintf("wf_%d", index),
			Description:  "中性描述",
			Reproduction: "中性复现步骤",
			Remediation:  "中性修复建议",
			EvidenceIDs:  []string{fmt.Sprintf("we_%d", index)},
		}
	}
	return reportDraftPayload{Title: "中性标题", Overview: "中性概述", Limitations: "中性局限", Items: items}
}

func validateReportDraftForErrorTest(arguments string) error {
	_, _, err := validateReportDraftArguments(arguments)
	return err
}

func TestWebAPIReportToolValidationErrors(t *testing.T) {
	valid := validReportDraftForErrorTest()
	if err := validateReportDraftForErrorTest(mustToolArgumentsJSON(t, valid)); err != nil {
		t.Fatalf("valid report draft rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*reportDraftPayload)
		parts  []string
	}{
		{"required title", func(args *reportDraftPayload) { args.Title = " \n" }, []string{"title", "必填为空"}},
		{"overview too long", func(args *reportDraftPayload) { args.Overview = strings.Repeat("o", 8193) }, []string{"overview", "8193", "8192"}},
		{"limitations too long", func(args *reportDraftPayload) { args.Limitations = strings.Repeat("l", 8193) }, []string{"limitations", "8193", "8192"}},
		{"empty items", func(args *reportDraftPayload) { args.Items = nil }, []string{"items", "实际 0", "至少 1"}},
		{"third reproduction too long", func(args *reportDraftPayload) { args.Items[2].Reproduction = strings.Repeat("r", 8193) }, []string{"items[2].reproduction", "8193", "8192"}},
		{"required item description", func(args *reportDraftPayload) { args.Items[1].Description = "" }, []string{"items[1].description", "必填为空"}},
		{"item remediation too long", func(args *reportDraftPayload) { args.Items[0].Remediation = strings.Repeat("m", 4097) }, []string{"items[0].remediation", "4097", "4096"}},
		{"missing finding id", func(args *reportDraftPayload) { args.Items[0].FindingID = "" }, []string{"items[0].finding_id", "必填为空"}},
		{"invalid finding id", func(args *reportDraftPayload) { args.Items[0].FindingID = "bad/id" }, []string{"items[0].finding_id", "非法"}},
		{"too much evidence", func(args *reportDraftPayload) {
			args.Items[1].EvidenceIDs = make([]string, 17)
			for index := range args.Items[1].EvidenceIDs {
				args.Items[1].EvidenceIDs[index] = fmt.Sprintf("we_extra_%d", index)
			}
		}, []string{"items[1].evidence_ids", "实际 17", "上限 16"}},
		{"duplicate evidence", func(args *reportDraftPayload) { args.Items[0].EvidenceIDs = []string{"we_same", "we_same"} }, []string{"items[0].evidence_ids[1]", "重复"}},
		{"invalid evidence", func(args *reportDraftPayload) { args.Items[0].EvidenceIDs = []string{"bad/id"} }, []string{"items[0].evidence_ids[0]", "非法"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := validReportDraftForErrorTest()
			test.mutate(&args)
			requireToolValidationError(t, validateReportDraftForErrorTest(mustToolArgumentsJSON(t, args)), test.parts...)
		})
	}

	t.Run("unknown field", func(t *testing.T) {
		raw := mustToolArgumentsJSON(t, validReportDraftForErrorTest())
		raw = strings.Replace(raw, `"title"`, `"unknown":true,"title"`, 1)
		requireToolValidationError(t, validateReportDraftForErrorTest(raw), "strict JSON", `未知字段 "unknown"`)
	})
	t.Run("type mismatch", func(t *testing.T) {
		raw := mustToolArgumentsJSON(t, validReportDraftForErrorTest())
		raw = strings.Replace(raw, `"title":"中性标题"`, `"title":7`, 1)
		requireToolValidationError(t, validateReportDraftForErrorTest(raw), "title", "期望 string", "实际 number", "字节位置")
	})
	t.Run("indexed type mismatch", func(t *testing.T) {
		raw := mustToolArgumentsJSON(t, validReportDraftForErrorTest())
		raw = strings.Replace(raw, `"finding_id":"wf_2","description":"中性描述","reproduction":"中性复现步骤"`, `"finding_id":"wf_2","description":"中性描述","reproduction":7`, 1)
		requireToolValidationError(t, validateReportDraftForErrorTest(raw), "items[2].reproduction", "期望 string", "实际 number", "字节位置")
	})
	t.Run("syntax offset", func(t *testing.T) {
		requireToolValidationError(t, validateReportDraftForErrorTest(`{"title":`), "JSON语法错误", "字节位置")
	})
}

func validFindingForErrorTest() findingToolArguments {
	return findingToolArguments{Content: findingContent{
		Title: "中性候选", Description: "中性主张", Remediation: "中性建议", Limitations: "中性局限",
		Steps:    []findingStepInput{{Text: "中性步骤", EvidenceIndices: []int{0}}},
		Evidence: []findingEvidenceInput{{EvidenceID: "we_0", Relation: "supporting"}},
	}}
}

func validateFindingForErrorTest(arguments string) error {
	_, _, err := validateFindingArguments(arguments)
	return err
}

func TestWebAPIFindingToolValidationErrors(t *testing.T) {
	valid := validFindingForErrorTest()
	if err := validateFindingForErrorTest(mustToolArgumentsJSON(t, valid)); err != nil {
		t.Fatalf("valid finding rejected: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*findingToolArguments)
		parts  []string
	}{
		{"required description", func(args *findingToolArguments) { args.Content.Description = " " }, []string{"content.description", "必填为空"}},
		{"second step too long", func(args *findingToolArguments) {
			args.Content.Steps = append(args.Content.Steps, findingStepInput{Text: strings.Repeat("s", 1025), EvidenceIndices: []int{0}})
		}, []string{"content.steps[1].text", "1025", "1024"}},
		{"too much evidence", func(args *findingToolArguments) {
			args.Content.Evidence = make([]findingEvidenceInput, 17)
			for index := range args.Content.Evidence {
				args.Content.Evidence[index] = findingEvidenceInput{EvidenceID: fmt.Sprintf("we_%d", index), Relation: "supporting"}
			}
		}, []string{"content.evidence", "实际 17", "上限 16"}},
		{"duplicate evidence", func(args *findingToolArguments) {
			args.Content.Evidence = append(args.Content.Evidence, args.Content.Evidence[0])
		}, []string{"content.evidence[1].evidence_id", "重复"}},
		{"invalid relation", func(args *findingToolArguments) { args.Content.Evidence[0].Relation = "other" }, []string{"content.evidence[0].relation", "仅支持"}},
		{"step index out of range", func(args *findingToolArguments) { args.Content.Steps[0].EvidenceIndices = []int{1} }, []string{"content.steps[0].evidence_indices[0]", "越界", "上限 0"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			args := validFindingForErrorTest()
			test.mutate(&args)
			requireToolValidationError(t, validateFindingForErrorTest(mustToolArgumentsJSON(t, args)), test.parts...)
		})
	}

	t.Run("unknown field", func(t *testing.T) {
		raw := strings.Replace(mustToolArgumentsJSON(t, validFindingForErrorTest()), `"content"`, `"unknown":true,"content"`, 1)
		requireToolValidationError(t, validateFindingForErrorTest(raw), "strict JSON", `未知字段 "unknown"`)
	})
	t.Run("type mismatch", func(t *testing.T) {
		raw := strings.Replace(mustToolArgumentsJSON(t, validFindingForErrorTest()), `"title":"中性候选"`, `"title":false`, 1)
		requireToolValidationError(t, validateFindingForErrorTest(raw), "content.title", "期望 string", "实际 boolean", "字节位置")
	})
	t.Run("text length remains byte limited", func(t *testing.T) {
		args := validFindingForErrorTest()
		args.Content.Title = strings.Repeat("中", 86)
		requireToolValidationError(t, validateFindingForErrorTest(mustToolArgumentsJSON(t, args)), "content.title", "实际 86 字符（258 字节）", "上限 256 字节")
	})
}

func validVerificationForErrorTest() verificationToolPayload {
	return verificationToolPayload{Verdict: "inconclusive", Method: "中性方法", Summary: "中性摘要"}
}

func validateVerificationForErrorTest(arguments string) error {
	_, _, err := validateVerificationArguments(arguments)
	return err
}

func TestWebAPIVerificationToolValidationErrors(t *testing.T) {
	if err := validateVerificationForErrorTest(mustToolArgumentsJSON(t, validVerificationForErrorTest())); err != nil {
		t.Fatalf("valid verification rejected: %v", err)
	}
	t.Run("required summary", func(t *testing.T) {
		args := validVerificationForErrorTest()
		args.Summary = "\t"
		requireToolValidationError(t, validateVerificationForErrorTest(mustToolArgumentsJSON(t, args)), "summary", "必填为空")
	})
	t.Run("too much evidence", func(t *testing.T) {
		args := validVerificationForErrorTest()
		args.Evidence = make([]verificationEvidenceInput, 17)
		for index := range args.Evidence {
			args.Evidence[index] = verificationEvidenceInput{EvidenceID: fmt.Sprintf("we_%d", index), Relation: "limitation"}
		}
		requireToolValidationError(t, validateVerificationForErrorTest(mustToolArgumentsJSON(t, args)), "evidence", "实际 17", "上限 16")
	})
	t.Run("duplicate evidence", func(t *testing.T) {
		args := validVerificationForErrorTest()
		args.Evidence = []verificationEvidenceInput{{EvidenceID: "we_same", Relation: "limitation"}, {EvidenceID: "we_same", Relation: "limitation"}}
		requireToolValidationError(t, validateVerificationForErrorTest(mustToolArgumentsJSON(t, args)), "evidence[1].evidence_id", "重复")
	})
	t.Run("unknown field", func(t *testing.T) {
		raw := strings.Replace(mustToolArgumentsJSON(t, validVerificationForErrorTest()), `"verdict"`, `"unknown":true,"verdict"`, 1)
		requireToolValidationError(t, validateVerificationForErrorTest(raw), "strict JSON", `未知字段 "unknown"`)
	})
	t.Run("type mismatch", func(t *testing.T) {
		raw := strings.Replace(mustToolArgumentsJSON(t, validVerificationForErrorTest()), `"method":"中性方法"`, `"method":[]`, 1)
		requireToolValidationError(t, validateVerificationForErrorTest(raw), "method", "期望 string", "实际 array", "字节位置")
	})
}

func validRetestForErrorTest() retestToolPayload {
	return retestToolPayload{Conclusion: "inconclusive", Method: "中性方法", Summary: "中性摘要"}
}

func validateRetestForErrorTest(arguments string) error {
	_, _, err := validateRetestArguments(arguments)
	return err
}

func TestWebAPIRetestToolValidationErrors(t *testing.T) {
	if err := validateRetestForErrorTest(mustToolArgumentsJSON(t, validRetestForErrorTest())); err != nil {
		t.Fatalf("valid retest rejected: %v", err)
	}
	t.Run("required method", func(t *testing.T) {
		args := validRetestForErrorTest()
		args.Method = ""
		requireToolValidationError(t, validateRetestForErrorTest(mustToolArgumentsJSON(t, args)), "method", "必填为空")
	})
	t.Run("type mismatch", func(t *testing.T) {
		raw := strings.Replace(mustToolArgumentsJSON(t, validRetestForErrorTest()), `"conclusion":"inconclusive"`, `"conclusion":1`, 1)
		requireToolValidationError(t, validateRetestForErrorTest(raw), "conclusion", "期望 string", "实际 number", "字节位置")
	})
	t.Run("invalid evidence relation", func(t *testing.T) {
		args := validRetestForErrorTest()
		args.Evidence = []retestEvidenceInput{{EvidenceID: "we_0", Relation: "other"}}
		requireToolValidationError(t, validateRetestForErrorTest(mustToolArgumentsJSON(t, args)), "evidence[0].relation", "仅支持")
	})
	t.Run("syntax offset", func(t *testing.T) {
		requireToolValidationError(t, validateRetestForErrorTest(`{"conclusion":`), "JSON语法错误", "字节位置")
	})
}
