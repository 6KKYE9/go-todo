# go-todo

零依赖的本地命令行待办看板，纯 Go 标准库实现，数据存于当前目录的 `todo.json`。
支持优先级、标签、截止日期，并自带网页界面（`-mode web`）。

## 安装 / 运行

```powershell
go run . add "写周报" -tag 工作 -pri high -due 2026-08-10
# 或编译后运行
go build -o todo.exe .
.\todo.exe list
```

## 子命令

| 命令 | 说明 |
| --- | --- |
| `add <标题> [-tag a,b] [-pri high\|mid\|low] [-due 2026-08-10]` | 加一条任务 |
| `list` | 列出全部（按优先级降序） |
| `search <关键词>` | 按标题搜索（忽略大小写） |
| `start <编号>` | 标记为进行中 |
| `done <编号>` | 标记为完成 |
| `rm <编号>` | 删除任务 |
| `-mode web [-addr :8080]` | 启动网页界面 |

## 网页界面

```powershell
go run . -mode web
```

打开浏览器访问 **http://localhost:8080**：

- 新增任务（标题 / 优先级 / 截止日期 / 标签）
- 看板按优先级排序，支持实时搜索
- 一键「进行中 / 完成 / 删除」
- 数据接口：`GET /api/list`、`GET /api/search?q=`、`POST /api/add`、`POST /api/done`、`POST /api/start`、`POST /api/rm`

## 测试

```powershell
go test ./...
```

覆盖命令行（解析、增删改、优先级排序）与 Web 接口（httptest）。

## 设计要点

- 数据与 `go-notes` 同一套路：`load`/`save` 拆出可注入路径版本，测试用临时文件零副作用。
- Web 层（`server.go`）直接复用 `cmdAdd`/`cmdDone`/`cmdStart`/`cmdRm`/`listTasks`，前后端共用同一份数据逻辑。
- 前端对用户输入做了 HTML 转义，防 XSS。
