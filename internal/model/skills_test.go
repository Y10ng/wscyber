package model

import "testing"

func TestSkillDecisionStrictPayloads(t *testing.T) {
	for _, raw := range []string{
		`{"kind":"skill_discover","summary":"按需查方法","next_step":"读取目录","skill":{}}`,
		`{"kind":"skill_load","summary":"按需查方法","next_step":"使用原件","skill":{"item_id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","source_token":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","purpose":"当前问题需要"}}`,
		`{"kind":"skill_release","summary":"不再需要","next_step":"继续研究","skill":{"load_id":"12345678-1234-4234-8234-123456789abc"}}`,
		`{"kind":"conclude","summary":"没有Skill也可正常结束"}`,
	} {
		if _, e := decodeDecision(raw); e != nil {
			t.Fatal(raw, e)
		}
	}
	for _, raw := range []string{
		`{"kind":"skill_discover","summary":"x","next_step":"x"}`,
		`{"kind":"skill_discover","summary":"x","next_step":"x","skill":null}`,
		`{"kind":"skill_discover","summary":"x","next_step":"x","skill":{"limit":21}}`,
		`{"kind":"skill_discover","summary":"x","next_step":"x","skill":{"query":"a","query":"b"}}`,
		`{"kind":"skill_discover","summary":"x","next_step":"x","skill":{"script":"evil"}}`,
		`{"kind":"skill_discover","summary":"x","next_step":"x","skill":{},"action":{"kind":"local_command","command":"x","reason":"x"}}`,
		`{"kind":"conclude","summary":"x","skill":{}}`,
	} {
		if _, e := decodeDecision(raw); e == nil {
			t.Fatal("非法Skill决策被接受", raw)
		}
	}
}
