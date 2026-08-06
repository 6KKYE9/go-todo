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

覆盖命令行（解析、增删改、优先级排序）与 Web 接口（httptest）。

## 设计要点

- 数据和 `go-notes` 同一套路：`load`/`save` 拆出可注入路径版本，测试用临时文件零副作用。
- Web 层（`server.go`）直接复用 `cmdAdd`/`cmdDone`/`cmdStart`/`cmdRm`/`listTasks`，前后端共用同一份数据逻辑。
- 前端对用户输入做了 HTML 转义，防 XSS。
