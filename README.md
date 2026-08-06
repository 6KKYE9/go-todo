# go-todo

本地命令行待办看板，数据存于当前目录的 `todo.json`。支持优先级、标签、截止日期，自带网页界面（`-mode web`）。

## 运行

```powershell
go run . add "写周报" -tag 工作 -pri high -due 2026-08-10
# 或编译后运行
go build -o todo.exe .
.\todo.exe list
```

## 子命令

| 命令 | 说明 |
| --- | --- |
| `add <标题> [-tag a,b] [-pri high\|mid\|low] [-due 2026-08-10] [-note 备注]` | 加一条任务 |
| `list [-tag 工作] [-status todo\|in_progress\|done]` | 列出任务（按优先级降序，可按标签/状态过滤） |
| `search <关键词>` | 按标题搜索（忽略大小写） |
| `start <编号>` | 标记为进行中 |
| `done <编号>` | 标记为完成 |
| `rm <编号>` | 删除任务 |
| `-mode web [-addr :8080]` | 启动网页界面 |

提示：有截止日期且未完成、且截止日早于今天的任务，列表会标注 `⚠逾期`；`add` 可加 `-note` 备注，列表与网页都会显示。

参数校验：`-due` 必须是 `YYYY-MM-DD`（`2026-02-30` 这种不存在的日期也会被拒绝），`-status` 只接受 `todo|in_progress|done`，编号必须是完整的正整数。出错时信息写 stderr 且退出码非 0，方便脚本里 `&&` 串联。

## 网页界面

```powershell
go run . -mode web
```

浏览器访问 **http://localhost:8080**：

- 新增任务（标题 / 优先级 / 截止日期 / 标签 / 备注）
- 看板按优先级排序，支持实时搜索
- 工具栏可按标签、按状态（待办/进行中/已完成）过滤
- 未完成任务截止日过期时在看板标注「⚠逾期」
- 一键「进行中 / 完成 / 删除」
- 数据接口：`GET /api/list?tag=&status=`、`GET /api/search?q=`、`POST /api/add`、`POST /api/done`、`POST /api/start`、`POST /api/rm`

## 测试

```powershell
go test ./...
```

覆盖命令行（解析、增删改、优先级排序、参数校验、并发写）与 Web 接口（httptest：方法校验、错误码、并发 add）。

## 设计要点

- 数据和 `go-notes` 同一套路：`load`/`save` 拆出可注入路径版本，测试用临时文件零副作用。
- Web 层（`server.go`）直接复用 `cmdAdd`/`cmdDone`/`cmdStart`/`cmdRm`/`listTasks`，前后端共用同一份数据逻辑，校验规则也共用一份（`parseID`/`validStatus`/`validateDue`）。
- 前端对用户输入做了 HTML 转义，防 XSS。

## 几处已修的坑

这一版专门修了几个会真的丢数据 / 让功能整个失效的问题，记在这里免得以后又踩：

- **并发写丢任务**：Web 模式下每个请求一个 goroutine，两个 `add` 各自「读全量 → 算新 ID → 写全量」，后写的会把先写的整个盖掉。写探针实测：20 个并发 `add` 最后**只剩 1 条**，19 条全没了，且 ID 全是 1。现在整段读改写用 `sync.Mutex` 串行化（本地小工具，性能不是瓶颈）。
- **写盘不原子**：原来直接 `os.WriteFile`，它会先把 `todo.json` 截断再写，中途崩溃/断电就只剩半截 JSON 甚至 0 字节，全部任务永久丢失。改成「写临时文件 → `Sync` → `Rename`」，同目录内 `Rename` 是原子的，要么全成要么保持原样。
- **网页端整个 JS 语法错误**：`<script>` 里多了一个 `}`，浏览器解析到那里直接报错，**整页所有按钮都不工作**。现在测试里加了括号配平检查，挡住同类低级错误。
- **`-due` 不校验**：`isOverdue` 是拿字符串直接比大小的，所以 `-due 9999` 永远不逾期、`-due 8/10/2026` 永远逾期。现在用 `time.Parse` 校验，顺带挡掉 `2026-02-30`。
- **`-status` 拼错静默返回空**：写成 `-status DONE` 只会得到「没有匹配的任务」，容易让人以为数据丢了。现在明确报错。
- **编号 `12abc` 被当成 12**：CLI 用的 `fmt.Sscanf("%d")` 会静默截断，Web 端用的 `strconv.Atoi` 不会——两端行为不一致。现在统一走 `parseID`。
- **出错也返回退出码 0**：`go-todo done 999 && echo ok` 会打印 ok。现在错误写 stderr 并 `os.Exit(1)`。
- **库函数里 `os.Exit`**：读文件失败直接杀进程，Web 模式下一次读失败就整个服务器没了。现在一律返回 `error`。
- **HTTP 层杂项**：`/api/list`、`/api/search` 补了 GET 方法校验；405 响应补 `Allow` 头；`/` 不再对任意未知路径返回整页 HTML；服务器换成显式 `http.Server` 并配上读写超时（原来的 `ListenAndServe(addr, nil)` 没有任何超时，一个卡住的连接会永久占着 goroutine 和 fd）；路由从 `DefaultServeMux` 换成独立 `mux`，测试里能反复起服务而不 panic。
