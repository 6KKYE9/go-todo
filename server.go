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
	"net/http"
	"strconv"
	"strings"
)

type taskJSON struct {
	ID        int      `json:"id"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags"`
	Priority  int      `json:"priority"`
	Status    string   `json:"status"`
	DueDate   string   `json:"due_date"`
	Note      string   `json:"note"`
	CreatedAt string   `json:"created_at"`
}

func toJSON(tasks []Task) []taskJSON {
	out := make([]taskJSON, 0, len(tasks))
	for _, t := range tasks {
		tags := t.Tags
		if tags == nil {
			tags = []string{}
		}
		out = append(out, taskJSON{
			ID:        t.ID,
			Title:     t.Title,
			Tags:      tags,
			Priority:  t.Priority,
			Status:    t.Status,
			DueDate:   t.DueDate,
			Note:      t.Note,
			CreatedAt: t.CreatedAt,
		})
	}
	return out
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(v)
}

func apiAdd(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST"})
		return
	}
	_ = r.ParseForm()
	title := strings.TrimSpace(r.FormValue("title"))
	if title == "" {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "title 不能为空"})
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
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"ok": out})
}

func apiList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	opt := listOptions{
		tag:    strings.TrimSpace(q.Get("tag")),
		status: strings.TrimSpace(q.Get("status")),
	}
	writeJSON(w, toJSON(filterTasks(listTasks(), opt)))
}

func apiSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeJSON(w, toJSON(listTasks()))
		return
	}
	tasks := load()
	q = strings.ToLower(q)
	var hit []Task
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.Title), q) {
			hit = append(hit, t)
		}
	}
	writeJSON(w, toJSON(hit))
}

func statusPost(w http.ResponseWriter, r *http.Request, action string, fn func(int) (string, error)) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		writeJSON(w, map[string]string{"error": "仅支持 POST"})
		return
	}
	_ = r.ParseForm()
	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": "id 需要是数字"})
		return
	}
	out, err := fn(id)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		writeJSON(w, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, map[string]string{"ok": out, "action": action})
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
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, indexHTML)
}

// StartServer 启动 Web 服务，监听 addr（如 ":8080"）。
func StartServer(addr string) error {
	http.HandleFunc("/", indexPage)
	http.HandleFunc("/api/add", apiAdd)
	http.HandleFunc("/api/list", apiList)
	http.HandleFunc("/api/search", apiSearch)
	http.HandleFunc("/api/done", apiDone)
	http.HandleFunc("/api/start", apiStart)
	http.HandleFunc("/api/rm", apiRm)
	fmt.Printf("go-todo 网页界面已启动： http://localhost%s\n", addr)
	return http.ListenAndServe(addr, nil)
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
      '<div class="meta">#' + t.id + ' · ' + statusText(t.status) + dueStr + noteStr + ' · ' + t.created_at + '</div>' +
      '<div class="ops">' + ops + '</div></div>';
  }).join('');
}
function todayStr() { return new Date().toISOString().slice(0,10); }
}
function statusText(s) { return s === 'done' ? '已完成' : (s === 'in_progress' ? '进行中' : '待办'); }
function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
}
loadTasks();
</script>
</body>
</html>`
