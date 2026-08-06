package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func postForm(t *testing.T, path string, body string) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	return w, req
}

// decodeTasks 解析任务列表响应。
func decodeTasks(t *testing.T, w *httptest.ResponseRecorder) []Task {
	t.Helper()
	var tasks []Task
	if err := json.Unmarshal(w.Body.Bytes(), &tasks); err != nil {
		t.Fatalf("解析响应失败: %v (body=%s)", err, w.Body.String())
	}
	return tasks
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
	tasks := decodeTasks(t, lr)
	if len(tasks) != 1 || tasks[0].Title != "写测试" || tasks[0].Priority != 3 {
		t.Fatalf("/api/list 数据异常: %+v", tasks)
	}

	wd, rd := postForm(t, "/api/done", "id=1")
	apiDone(wd, rd)
	if wd.Code != http.StatusOK {
		t.Fatalf("/api/done 状态码 %d", wd.Code)
	}
	tasks2 := mustLoad(t)
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

func TestAPIAddBadDue(t *testing.T) {
	withTempData(t)
	w, r := postForm(t, "/api/add", "title=任务&due=9999")
	apiAdd(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法截止日应 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestAPIMethodNotAllowed(t *testing.T) {
	withTempData(t)
	// 写接口必须是 POST
	for _, p := range []struct {
		name string
		fn   http.HandlerFunc
		path string
	}{
		{"add", apiAdd, "/api/add"},
		{"done", apiDone, "/api/done"},
		{"start", apiStart, "/api/start"},
		{"rm", apiRm, "/api/rm"},
	} {
		w := httptest.NewRecorder()
		p.fn(w, httptest.NewRequest(http.MethodGet, p.path, nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s 非 POST 应 405, got %d", p.path, w.Code)
		}
		if got := w.Header().Get("Allow"); got != http.MethodPost {
			t.Fatalf("%s 405 响应应带 Allow: POST, got %q", p.path, got)
		}
	}
	// 读接口必须是 GET
	for _, p := range []struct {
		fn   http.HandlerFunc
		path string
	}{
		{apiList, "/api/list"},
		{apiSearch, "/api/search"},
	} {
		w := httptest.NewRecorder()
		p.fn(w, httptest.NewRequest(http.MethodPost, p.path, nil))
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s 非 GET 应 405, got %d", p.path, w.Code)
		}
	}
}

// id 校验以前用 strconv.Atoi 直收，但 CLI 用 Sscanf 会接受 "12abc"；
// 现在两端统一走 parseID，非法值一律 400。
func TestAPIBadID(t *testing.T) {
	withTempData(t)
	for _, id := range []string{"", "abc", "12abc", "0", "-1"} {
		w, r := postForm(t, "/api/done", "id="+id)
		apiDone(w, r)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("id=%q 应 400, got %d body=%s", id, w.Code, w.Body.String())
		}
	}
}

func TestAPIListBadStatus(t *testing.T) {
	withTempData(t)
	w := httptest.NewRecorder()
	apiList(w, httptest.NewRequest(http.MethodGet, "/api/list?status=DONE", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法 status 应 400, got %d", w.Code)
	}
}

func TestAPISearch(t *testing.T) {
	withTempData(t)
	mustAdd(t, "学习 Rust")
	mustAdd(t, "买水果")
	r := httptest.NewRecorder()
	apiSearch(r, httptest.NewRequest(http.MethodGet, "/api/search?q=rust", nil))
	hits := decodeTasks(t, r)
	if len(hits) != 1 || hits[0].Title != "学习 Rust" {
		t.Fatalf("搜索结果异常: %+v", hits)
	}
	// q 为空时返回全部
	all := httptest.NewRecorder()
	apiSearch(all, httptest.NewRequest(http.MethodGet, "/api/search", nil))
	if len(decodeTasks(t, all)) != 2 {
		t.Fatalf("空 q 应返回全部: %s", all.Body.String())
	}
}

// 无标签的任务不该把 tags 编码成 null，前端和其他 API 消费者都更希望看到 []。
func TestAPIListTagsNeverNull(t *testing.T) {
	withTempData(t)
	mustAdd(t, "没有标签的任务")
	w := httptest.NewRecorder()
	apiList(w, httptest.NewRequest(http.MethodGet, "/api/list", nil))
	if strings.Contains(w.Body.String(), `"tags":null`) {
		t.Fatalf("tags 不应为 null: %s", w.Body.String())
	}
}

func TestIndexHasAPI(t *testing.T) {
	w := httptest.NewRecorder()
	indexPage(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if !strings.Contains(w.Body.String(), "/api/add") || !strings.Contains(w.Body.String(), "/api/rm") {
		t.Fatal("首页应挂载 /api/add 与 /api/rm")
	}
}

// "/" 是兜底模式，未注册路径以前也会返回整页 HTML。
func TestIndexUnknownPath404(t *testing.T) {
	w := httptest.NewRecorder()
	indexPage(w, httptest.NewRequest(http.MethodGet, "/favicon.ico", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知路径应 404, got %d", w.Code)
	}
}

// 页面里的 JS 有一段花括号写错过，整个 <script> 直接语法错误、全站按钮失效。
// 这里做个粗粒度的括号配平检查，能挡住同类低级错误。
func TestIndexScriptBracesBalanced(t *testing.T) {
	start := strings.Index(indexHTML, "<script>")
	end := strings.Index(indexHTML, "</script>")
	if start < 0 || end < 0 || end < start {
		t.Fatal("首页应包含 <script> 段")
	}
	js := indexHTML[start+len("<script>") : end]
	depth := 0
	for _, c := range js {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth < 0 {
				t.Fatal("JS 里出现多余的 }，脚本会整段语法错误")
			}
		}
	}
	if depth != 0 {
		t.Fatalf("JS 花括号不配平, 剩余 %d 个未闭合", depth)
	}
}

func TestNewMuxRoutes(t *testing.T) {
	withTempData(t)
	mux := newMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/list")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/api/list 应 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("Content-Type 应为 JSON, got %q", ct)
	}
}

// Web 模式下每个请求一个 goroutine，并发 add 必须一条都不丢。
func TestAPIConcurrentAdd(t *testing.T) {
	withTempData(t)
	const n = 15
	var wg sync.WaitGroup
	codes := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w, r := postForm(t, "/api/add", "title=并发")
			apiAdd(w, r)
			codes[i] = w.Code
		}(i)
	}
	wg.Wait()
	for i, c := range codes {
		if c != http.StatusOK {
			t.Fatalf("第 %d 个请求状态码 %d", i, c)
		}
	}
	if got := len(mustLoad(t)); got != n {
		t.Fatalf("并发 add 后应有 %d 条, got %d", n, got)
	}
}
