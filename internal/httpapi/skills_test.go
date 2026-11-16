package httpapi

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"wscyber/internal/domain"
)

type skillHTTPRepo struct {
	domain.Repository
	domain.SkillRepository
	calls                int
	mission, load, after string
	limit                int
	err                  error
}

func (r *skillHTTPRepo) SkillUsage(_ context.Context, mission, after string, limit int) (domain.SkillUsagePage, error) {
	r.calls++
	r.mission = mission
	r.after = after
	r.limit = limit
	return domain.SkillUsagePage{MissionID: mission, TotalSuccessfulLoads: "9007199254740993", Items: []domain.SkillUsageItem{}}, r.err
}
func (r *skillHTTPRepo) ListSkillActivities(_ context.Context, mission, after string, limit int) (domain.SkillActivityPage, error) {
	r.calls++
	r.mission = mission
	r.after = after
	r.limit = limit
	return domain.SkillActivityPage{MissionID: mission, Items: []domain.SkillActivity{}}, r.err
}
func (r *skillHTTPRepo) GetSkillLoad(_ context.Context, mission, load string) (domain.SkillLoad, error) {
	r.calls++
	r.mission = mission
	r.load = load
	return domain.SkillLoad{MissionID: mission, LoadID: load}, r.err
}
func (r *skillHTTPRepo) SkillModelCalls(_ context.Context, mission, load, after string, limit int) (domain.SkillCallsPage, error) {
	r.calls++
	r.mission = mission
	r.load = load
	r.after = after
	r.limit = limit
	return domain.SkillCallsPage{MissionID: mission, LoadID: load, Items: []domain.ModelCall{}}, r.err
}
func TestSkillReadRoutesAndQueryBoundaries(t *testing.T) {
	repo := &skillHTTPRepo{}
	handler := New(repo, nil, Config{Token: testToken})
	base := "/api/missions/11111111-1111-1111-1111-111111111111/"
	load := "22222222-2222-2222-2222-222222222222"
	for _, path := range []string{"skill-usage", "skill-activities", "skill-loads/" + load, "skill-loads/" + load + "/calls"} {
		t.Run(path, func(t *testing.T) {
			before := repo.calls
			w := request(handler, "GET", base+path, "", "")
			if w.Code != 401 || repo.calls != before {
				t.Fatal("未认证仍读取", w.Code)
			}
			w = request(handler, "GET", base+path, "", testToken)
			if w.Code != 200 || repo.calls != before+1 || !strings.Contains(w.Header().Get("Cache-Control"), "no-store") {
				t.Fatal(w.Code, w.Header())
			}
			if repo.mission != "11111111-1111-1111-1111-111111111111" {
				t.Fatal(repo.mission)
			}
			for _, query := range []string{"limit=101", "limit=0", "limit=", "limit=1&limit=2", "after=a&after=b", "unknown=1", "q=%ZZ"} {
				before = repo.calls
				w = request(handler, "GET", base+path+"?"+query, "", testToken)
				if w.Code != 400 || repo.calls != before {
					t.Fatal(query, w.Code)
				}
			}
			repo.err = domain.ErrNotFound
			w = request(handler, "GET", base+path, "", testToken)
			repo.err = nil
			if w.Code != 404 {
				t.Fatal("未保留跨项目/不存在404", w.Code)
			}
		})
	}
	after, limit, err := parseSkillPage(httptest.NewRequest("GET", "/?after=9&limit=100", nil))
	if err != nil || after != "9" || limit != 100 {
		t.Fatal(after, limit, err)
	}
	w := request(handler, "GET", base+"skill-usage", "", testToken)
	if !strings.Contains(w.Body.String(), `"9007199254740993"`) {
		t.Fatal("计数转成了浮点数", w.Body.String())
	}
}
