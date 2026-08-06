package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func postForm(t *testing.T, path string, body string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	return w, req
}

func TestAPIAddListAndStatus(t *testing.T) {
	withTempData(t)
	w, r := postForm(t, "/api/add", "title=写测试&pri=high&due=2026-08-10&tags=工作")
	apiAdd(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/add 状态码 %d body=%s", w.Code, w.Body.String())
	}
	lr := httptest.NewRecorder()
	apiList(lr, httptest.NewRequest(http.MethodGet, "/api/list", nil))
	var tasks []taskJSON
	if err := json.Unmarshal(lr.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("解析 /api/list 失败: %v", err)
	}
	if len(tasks) != 1 || tasks[0].Title != "写测试" || tasks[0].Priority != 3 {
		t.Fatalf("/api/list 数据异常: %+v", tasks)
	}

	wd, rd := postForm(t, "/api/done", "id=1")
	apiDone(wd, rd)
	if wd.Code != http.StatusOK {
		t.Fatalf("/api/done 状态码 %d", wd.Code)
	}
	tasks2 := load()
	if tasks2[0].Status != "done" {
		t.Fatalf("标记完成未生效: %s", tasks2[0].Status)
	}
}

func TestAPIAddEmptyTitle(t *testing.T) {
	withTempData(t)
	w, r := postForm(t, "/api/add", "title=")
	apiAdd(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("空标题应 400, got %d", w.Code)
	}
}

func TestAPIMethodNotAllowed(t *testing.T) {
	withTempData(t)
	w := httptest.NewRecorder()
	apiAdd(w, httptest.NewRequest(http.MethodGet, "/api/add", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("/api/add 非 POST 应 405, got %d", w.Code)
	}
}

func TestAPISearch(t *testing.T) {
	withTempData(t)
	cmdAdd([]string{"学习 Rust"})
	cmdAdd([]string{"买水果"})
	r := httptest.NewRecorder()
	apiSearch(r, httptest.NewRequest(http.MethodGet, "/api/search?q=rust", nil))
	var hits []taskJSON
	_ = json.Unmarshal(r.Body.Bytes(), &hits)
	if len(hits) != 1 || hits[0].Title != "学习 Rust" {
		t.Fatalf("搜索结果异常: %+v", hits)
	}
}

func TestIndexHasAPI(t *testing.T) {
	w := httptest.NewRecorder()
	indexPage(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(w.Body.String(), "/api/add") || !strings.Contains(w.Body.String(), "/api/rm") {
		t.Fatal("首页应挂载 /api/add 与 /api/rm")
	}
}
