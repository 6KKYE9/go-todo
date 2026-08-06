package main

// server.go 提供 Web 界面（纯标准库 net/http，无需任何 CGO / 前端框架）：
//   - GET  /              返回任务看板页面
//   - POST /api/add       新增任务（表单 title/tags/pri/due）
//   - GET  /api/list      返回全部任务 JSON
//   - GET  /api/search    按标题搜索（q 参数）
//   - POST /api/done      标记完成（表单 id）
//   - POST /api/start     标记进行中（表单 id）
//   - POST /api/rm        删除任务（表单 id）
//
// 复用命令行的 load/cmdAdd/cmdDone/cmdStart/cmdRm，零额外依赖。

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

// toJSON 把内部 Task 转成给前端的形态。
// 原来这里另外定义了一份字段完全相同的 taskJSON 结构体，
// 两处 tag 一旦改歪就会静默不一致；直接复用 Task 即可，
// 唯一需要处理的是 nil Tags —— 它会被编码成 null，
// 前端 (t.tags || []) 虽然兜住了，但 API 消费者不该看到 null。
func toJSON(tasks []Task) []Task {
	out := make([]Task, 0, len(tasks))
	for _, t := range tasks {
		if t.Tags == nil {
			t.Tags = []string{}
		}
		out = append(out, t)
	}
	return out
}

func writeJSON(w http.ResponseWriter, code int, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// 响应头已发出，只能记日志，不能再改状态码。
		log.Printf("写响应失败: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// requirePost 拦掉非 POST 的写操作，并带上 Allow 头（RFC 要求 405 必须给）。
func requirePost(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 POST")
		return false
	}
	return true
}

// requireGet 拦掉非 GET 的读操作。
func requireGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		writeErr(w, http.StatusMethodNotAllowed, "仅支持 GET")
		return false
	}
	return true
}

// readTasks 在锁内读一次任务，避免和写请求撞上读到半截数据。
func readTasks() ([]Task, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	return listTasks()
}

func apiAdd(w http.ResponseWriter, r *http.Request) {
	if !requirePost(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeErr(w, http.StatusBadRequest, "表单解析失败: "+err.Error())
		return
	}
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		writeErr(w, http.StatusBadRequest, "title 不能为空")
		return
	}
	var args []string
	args = append(args, title)
	if tags := strings.TrimSpace(r.FormValue("tags")); tags != "" {
		args = append(args, "-tag", tags)
	}
	if pri := strings.TrimSpace(r.FormValue("pri")); pri != "" {
		args = append(args, "-pri", pri)
	}
	if due := strings.TrimSpace(r.FormValue("due")); due != "" {
		args = append(args, "-due", due)
	}
	if note := strings.TrimSpace(r.FormValue("note")); note != "" {
		args = append(args, "-note", note)
	}
	out, err := cmdAdd(args)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": out})
}

func apiList(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	q := r.URL.Query()
	opt := listOptions{
		tag:    strings.TrimSpace(q.Get("tag")),
		status: strings.TrimSpace(q.Get("status")),
	}
	// 状态过滤值同样做白名单，和 CLI 的 cmdList 保持一致。
	if opt.status != "" && !validStatus(opt.status) {
		writeErr(w, http.StatusBadRequest, "status 应为 todo|in_progress|done")
		return
	}
	tasks, err := readTasks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, toJSON(filterTasks(tasks, opt)))
}

func apiSearch(w http.ResponseWriter, r *http.Request) {
	if !requireGet(w, r) {
		return
	}
	tasks, err := readTasks()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, http.StatusOK, toJSON(tasks))
		return
	}
	q = strings.ToLower(q)
	var hit []Task
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.Title), q) {
			hit = append(hit, t)
		}
	}
	writeJSON(w, http.StatusOK, toJSON(hit))
}

func statusPost(w http.ResponseWriter, r *http.Request, action string, fn func(int) (string, error)) {
	if !requirePost(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		writeErr(w, http.StatusBadRequest, "表单解析失败: "+err.Error())
		return
	}
	// 复用 CLI 的 parseID，"12abc"/"-1" 在两端都会被同样拒绝。
	id, err := parseID(r.FormValue("id"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	out, err := fn(id)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": out, "action": action})
}

func apiDone(w http.ResponseWriter, r *http.Request) {
	statusPost(w, r, "done", cmdDone)
}

func apiStart(w http.ResponseWriter, r *http.Request) {
	statusPost(w, r, "start", cmdStart)
}

func apiRm(w http.ResponseWriter, r *http.Request) {
	statusPost(w, r, "rm", cmdRm)
}

func indexPage(w http.ResponseWriter, r *http.Request) {
	// "/" 在 ServeMux 里是兜底模式，任何未注册路径都会落到这里。
	// 不判断的话访问 /favicon.ico 也会返回整页 HTML。
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if !requireGet(w, r) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

// newMux 组装路由。独立成函数，测试里可以直接拿到一个干净的 mux，
// 不用碰全局的 http.DefaultServeMux（重复注册会 panic，多个测试就跑不起来）。
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", indexPage)
	mux.HandleFunc("/api/add", apiAdd)
	mux.HandleFunc("/api/list", apiList)
	mux.HandleFunc("/api/search", apiSearch)
	mux.HandleFunc("/api/done", apiDone)
	mux.HandleFunc("/api/start", apiStart)
	mux.HandleFunc("/api/rm", apiRm)
	return mux
}

// StartServer 启动 Web 服务，监听 addr（如 ":8080"）。
func StartServer(addr string) error {
	// 显式建 http.Server 而不是 ListenAndServe(addr, nil)：
	// 后者没有任何超时，一个卡住的连接会永久占着 goroutine 和 fd。
	srv := &http.Server{
		Addr:              addr,
		Handler:           newMux(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Printf("go-todo 网页界面已启动： http://localhost%s\n", addr)
	return srv.ListenAndServe()
}

const indexHTML = `<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>go-todo 待办看板</title>
<style>
  * { box-sizing: border-box; }
  body { font-family: -apple-system, "Microsoft YaHei", sans-serif; margin: 0;
         background: linear-gradient(135deg,#11998e,#38ef7d); color: #143; min-height: 100vh; }
  .wrap { max-width: 800px; margin: 0 auto; padding: 32px 16px; }
  h1 { font-size: 26px; margin-bottom: 4px; color:#0b3d2e; }
  .sub { opacity: .8; margin-bottom: 22px; font-size: 14px; }
  .card { background: rgba(255,255,255,.92); border-radius: 12px; padding: 18px;
          box-shadow: 0 8px 24px rgba(0,0,0,.12); margin-bottom: 18px; }
  .row { display: flex; gap: 10px; flex-wrap: wrap; }
  .row > div { flex: 1; min-width: 140px; }
  label { display: block; font-size: 13px; color:#0b3d2e; margin-bottom: 6px; font-weight:600; }
  input, select { width: 100%; padding: 10px 12px; border: 1px solid #cfe; border-radius: 8px; font-size: 14px; }
  button { margin-top: 12px; width: 100%; padding: 11px; border: none; border-radius: 8px;
           background: #11998e; color: #fff; font-size: 15px; font-weight: 700; cursor: pointer; }
  button:hover { background: #0e7d74; }
  .toolbar { display: flex; gap: 10px; margin-bottom: 14px; }
  .toolbar input { flex: 1; }
  .toolbar button { width: auto; margin: 0; padding: 10px 18px; }
  .task { background: #fff; border-left: 5px solid #38ef7d; border-radius: 8px;
          padding: 12px 14px; margin-bottom: 10px; box-shadow: 0 2px 8px rgba(0,0,0,.06); }
  .task.done { border-left-color: #bbb; opacity: .55; }
  .task.in_progress { border-left-color: #f6c244; }
  .task .top { display: flex; justify-content: space-between; align-items: center; gap: 10px; }
  .task .title { font-size: 15px; font-weight: 600; }
  .task .meta { font-size: 12px; color: #678; margin-top: 6px; }
  .task .tags span { background: #e6f7f1; color: #11998e; padding: 2px 8px;
                     border-radius: 10px; font-size: 12px; margin-right: 6px; }
  .pri { font-size: 11px; padding: 2px 8px; border-radius: 10px; color:#fff; }
  .pri-3 { background: #e74c3c; } .pri-2 { background: #f39c12; } .pri-1 { background: #95a5a6; }
  .ops { margin-top: 8px; display: flex; gap: 8px; }
  .ops button { width: auto; margin: 0; padding: 6px 14px; font-size: 13px; }
  .ops .danger { background: #e74c3c; }
  #status { margin-top: 10px; font-size: 14px; min-height: 18px; color:#0b3d2e; }
</style>
</head>
<body>
<div class="wrap">
  <h1>✅ go-todo 待办看板</h1>
  <div class="sub">本地纯标准库实现，数据存于 todo.json，支持优先级/标签/截止日。</div>

  <div class="card">
    <div class="row">
      <div style="flex:3">
        <label>任务标题</label>
        <input id="title" placeholder="今天要做的事…">
      </div>
      <div>
        <label>优先级</label>
        <select id="pri">
          <option value="high">高</option>
          <option value="mid" selected>中</option>
          <option value="low">低</option>
        </select>
      </div>
      <div>
        <label>截止日期</label>
        <input id="due" placeholder="2026-08-10（可空）">
      </div>
      <div>
        <label>标签（逗号分隔）</label>
        <input id="tags" placeholder="工作,会议">
      </div>
      <div style="flex:2">
        <label>备注</label>
        <input id="note" placeholder="可选备注…">
      </div>
    </div>
    <button onclick="addTask()">添加任务</button>
    <div id="status"></div>
  </div>

  <div class="toolbar">
    <input id="q" placeholder="搜索关键词…" oninput="searchTasks()">
    <input id="ftag" placeholder="按标签过滤 (如 工作)" oninput="loadTasks()">
    <select id="fstatus" onchange="loadTasks()">
      <option value="">全部状态</option>
      <option value="todo">待办</option>
      <option value="in_progress">进行中</option>
      <option value="done">已完成</option>
    </select>
    <button onclick="loadTasks()">显示全部</button>
  </div>

  <div id="list"></div>
</div>
<script>
function status(msg, cls) {
  const s = document.getElementById('status');
  s.textContent = msg; s.className = cls || '';
}
function loadTasks() {
  document.getElementById('q').value = '';
  const tag = document.getElementById('ftag').value.trim();
  const status = document.getElementById('fstatus').value;
  let url = '/api/list';
  const params = [];
  if (tag) params.push('tag=' + encodeURIComponent(tag));
  if (status) params.push('status=' + encodeURIComponent(status));
  if (params.length) url += '?' + params.join('&');
  fetch(url).then(r => r.json()).then(render).catch(e => status('加载失败：' + e, 'err'));
}
function searchTasks() {
  const q = document.getElementById('q').value.trim();
  const url = q ? '/api/search?q=' + encodeURIComponent(q) : '/api/list';
  fetch(url).then(r => r.json()).then(render).catch(e => status('搜索失败：' + e, 'err'));
}
function addTask() {
  const title = document.getElementById('title').value.trim();
  const pri = document.getElementById('pri').value;
  const due = document.getElementById('due').value.trim();
  const tags = document.getElementById('tags').value.trim();
  const note = document.getElementById('note').value.trim();
  if (!title) { status('标题不能为空', 'err'); return; }
  const fd = new URLSearchParams();
  fd.append('title', title); fd.append('pri', pri); fd.append('due', due); fd.append('tags', tags); fd.append('note', note);
  fetch('/api/add', {method:'POST', body: fd})
    .then(r => r.json().then(d => ({ok: r.ok, d})))
    .then(({ok, d}) => {
      if (!ok) { status('出错：' + d.error, 'err'); return; }
      status(d.ok, 'ok');
      document.getElementById('title').value = '';
      document.getElementById('due').value = '';
      document.getElementById('tags').value = '';
      document.getElementById('note').value = '';
      loadTasks();
    })
    .catch(e => status('添加失败：' + e, 'err'));
}
function act(url, id) {
  const fd = new URLSearchParams(); fd.append('id', id);
  fetch(url, {method:'POST', body: fd})
    .then(r => r.json().then(d => ({ok: r.ok, d})))
    .then(({ok, d}) => { if (!ok) { status('出错：' + d.error, 'err'); return; } status(d.ok, 'ok'); loadTasks(); })
    .catch(e => status('操作失败：' + e, 'err'));
}
function render(tasks) {
  const box = document.getElementById('list');
  if (!tasks || !tasks.length) { box.innerHTML = '<div class="task">还没有任务，先添加一条吧。</div>'; return; }
  box.innerHTML = tasks.map(t => {
    const tags = (t.tags || []).map(x => '<span>#' + escapeHtml(x) + '</span>').join('');
    const cls = t.status === 'done' ? 'task done' : (t.status === 'in_progress' ? 'task in_progress' : 'task');
    let dueStr = t.due_date ? ' · 截止 ' + escapeHtml(t.due_date) : '';
    if (t.due_date && t.status !== 'done' && t.due_date < todayStr()) dueStr += ' · <span style="color:#e74c3c;font-weight:600">⚠逾期</span>';
    const noteStr = t.note ? ' · 备注 ' + escapeHtml(t.note) : '';
    let ops = '';
    if (t.status !== 'done') ops += '<button onclick="act(\'/api/done\',' + t.id + ')">完成</button>';
    if (t.status !== 'in_progress' && t.status !== 'done') ops += '<button onclick="act(\'/api/start\',' + t.id + ')">进行中</button>';
    ops += '<button class="danger" onclick="act(\'/api/rm\',' + t.id + ')">删除</button>';
    return '<div class="' + cls + '">' +
      '<div class="top"><div class="title">' + escapeHtml(t.title) + '</div>' +
      '<span class="pri pri-' + t.priority + '">' + (t.priority===3?'高':(t.priority===2?'中':'低')) + '</span></div>' +
      (tags ? '<div class="tags">' + tags + '</div>' : '') +
      '<div class="meta">#' + t.id + ' · ' + statusText(t.status) + dueStr + noteStr + ' · ' + escapeHtml(t.created_at) + '</div>' +
      '<div class="ops">' + ops + '</div></div>';
  }).join('');
}
function todayStr() { return new Date().toISOString().slice(0,10); }
function statusText(s) { return s === 'done' ? '已完成' : (s === 'in_progress' ? '进行中' : '待办'); }
function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}
loadTasks();
</script>
</body>
</html>`
