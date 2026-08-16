// go-todo 是一个零依赖的本地命令行待办看板。
// 数据落在当前目录的 todo.json 里，不依赖任何数据库或外部服务。
// 支持 add（加任务，可带优先级/标签/截止日）、list（列表）、
// done（标记完成）、rm（删除）、search（搜索）。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Task 是一条待办任务。
type Task struct {
	ID        int      `json:"id"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags"`
	Priority  int      `json:"priority"` // 1=低 2=中 3=高
	Status    string   `json:"status"`   // todo / in_progress / done
	DueDate   string   `json:"due_date"` // 截止日期，空表示无
	Note      string   `json:"note"`     // 备注，空表示无
	CreatedAt string   `json:"created_at"`
}

// todayStr 返回本地日期（用于逾期判断）。
func todayStr() string {
	return time.Now().Format("2006-01-02")
}

// isOverdue 判断任务是否已逾期：有截止日、未完成、且截止日早于今天。
func (t Task) isOverdue() bool {
	if t.DueDate == "" || t.Status == "done" {
		return false
	}
	return t.DueDate < todayStr()
}

// dataFile 默认数据文件。测试时会被替换成临时文件。
var dataFile = "todo.json"

// storeMu 保护 todo.json 的"读—改—写"整段。
// Web 模式下每个请求一个 goroutine，两个并发 add 若各自读到同样的数据，
// 会算出同一个新 ID，后写的把先写的整个覆盖掉——任务丢失且编号重复。
// 加锁把整段串行化即可避免（本地小工具，性能不是瓶颈）。
var storeMu sync.Mutex

// loadFromFile 读取指定路径的任务；文件不存在时返回空切片而不是报错。
// 出错返回 error 而不是直接退出进程：这是个库函数，Web 模式下
// 不该因为一次读失败就把整个服务器杀掉。
func loadFromFile(path string) ([]Task, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}, nil
		}
		return nil, fmt.Errorf("读取数据失败: %w", err)
	}
	if len(b) == 0 {
		return []Task{}, nil
	}
	var tasks []Task
	if err := json.Unmarshal(b, &tasks); err != nil {
		return nil, fmt.Errorf("数据文件损坏: %w", err)
	}
	return tasks, nil
}

func load() ([]Task, error) { return loadFromFile(dataFile) }

// saveToFile 把任务写到指定路径，带缩进方便人直接看。
//
// 用"写临时文件 + Sync + Rename"而不是直接 os.WriteFile：
// WriteFile 会先把原文件截断再写，中途崩溃/断电就只剩半截 JSON 甚至空文件，
// 全部任务永久丢失。Rename 在同一目录内是原子的，要么全成要么保持原样。
func saveToFile(tasks []Task, path string) error {
	b, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化失败: %w", err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".todo-*.tmp")
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	tmpName := tmp.Name()
	// 任何一步失败都要清掉临时文件，别在用户目录里留垃圾。
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("写入失败: %w", err)
	}
	// Sync 保证数据真正落盘，否则 Rename 后仍可能丢内容。
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("刷盘失败: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return fmt.Errorf("设置权限失败: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("替换数据文件失败: %w", err)
	}
	tmpName = "" // 已改名成功，不需要再删
	return nil
}

func save(tasks []Task) error { return saveToFile(tasks, dataFile) }

// nextID 取现有最大 ID + 1，避免出现重复编号。
func nextID(tasks []Task) int {
	max := 0
	for _, t := range tasks {
		if t.ID > max {
			max = t.ID
		}
	}
	return max + 1
}

// parseTags 把 "a,b,c" 或 "a b c" 拆成标签切片。
func parseTags(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parsePriority 把 low/mid/high 或数字映射成 1/2/3。
func parsePriority(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high", "h", "3":
		return 3
	case "mid", "m", "medium", "2":
		return 2
	case "low", "l", "1":
		return 1
	case "":
		return 2
	default:
		n := 0
		fmt.Sscanf(s, "%d", &n)
		if n < 1 {
			n = 1
		}
		if n > 3 {
			n = 3
		}
		return n
	}
}

func priorityText(p int) string {
	switch p {
	case 3:
		return "高"
	case 2:
		return "中"
	default:
		return "低"
	}
}

// cmdAdd 加一条任务，返回结果文案或错误。
func cmdAdd(args []string) (string, error) {
	if len(args) < 1 {
		return "", fmt.Errorf("用法: go-todo add <标题> [-tag a,b] [-pri high|mid|low] [-due 2026-08-10]")
	}
	tags := []string{}
	pri := 2
	due := ""
	note := ""
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-tag":
			if i+1 < len(args) {
				tags = parseTags(args[i+1])
				i++
			}
		case "-pri", "-priority":
			if i+1 < len(args) {
				pri = parsePriority(args[i+1])
				i++
			}
		case "-due":
			if i+1 < len(args) {
				due = strings.TrimSpace(args[i+1])
				i++
			}
		case "-note":
			if i+1 < len(args) {
				note = strings.TrimSpace(args[i+1])
				i++
			}
		default:
			rest = append(rest, args[i])
		}
	}
	if len(rest) == 0 {
		return "", fmt.Errorf("用法: go-todo add <标题> [-tag a,b] [-pri high|mid|low] [-due 2026-08-10] [-note 备注]")
	}
	// 截止日必须能被解析，否则 isOverdue 的字符串比较会给出无意义的结果
	// （比如 "9999" 永不逾期、"1/2/2026" 永远逾期）。
	if due != "" {
		if err := validateDue(due); err != nil {
			return "", err
		}
	}
	title := strings.Join(rest, " ")

	storeMu.Lock()
	defer storeMu.Unlock()

	tasks, err := load()
	if err != nil {
		return "", err
	}
	t := Task{
		ID:        nextID(tasks),
		Title:     title,
		Tags:      tags,
		Priority:  pri,
		Status:    "todo",
		DueDate:   due,
		Note:      note,
		CreatedAt: time.Now().Format("2006-01-02 15:04"),
	}
	tasks = append(tasks, t)
	if err := save(tasks); err != nil {
		return "", err
	}
	return fmt.Sprintf("已添加 #%d：%s", t.ID, t.Title), nil
}

// validateDue 校验截止日期必须是 YYYY-MM-DD。
// 这里用 time.Parse 而不是正则，顺便把 2026-02-30 这种不存在的日期也挡掉。
func validateDue(s string) error {
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return fmt.Errorf("截止日期格式应为 YYYY-MM-DD，收到 %q", s)
	}
	return nil
}

// parseID 把命令行参数解析成任务编号。
// 原来用 fmt.Sscanf("%d")，"12abc" 会被当成 12 静默接受；
// strconv.Atoi 要求整串都是数字，与 Web 端行为一致。
func parseID(s string) (int, error) {
	id, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("编号应为整数，收到 %q", s)
	}
	if id < 1 {
		return 0, fmt.Errorf("编号应为正整数，收到 %d", id)
	}
	return id, nil
}

// listTasks 返回当前所有任务（按优先级降序、ID 升序），供展示与测试复用。
func listTasks() ([]Task, error) {
	tasks, err := load()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority > tasks[j].Priority
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks, nil
}

// listOptions 是 list 的过滤选项。
type listOptions struct {
	tag    string // 只显示含该标签的任务（空=不过滤）
	status string // 只显示该状态的任务（空=不过滤）
}

// filterTasks 按选项过滤任务；tag/status 为空表示不过滤。
// 不过滤时返回拷贝而非原切片，避免调用方改动结果时波及底层数组。
func filterTasks(tasks []Task, opt listOptions) []Task {
	if opt.tag == "" && opt.status == "" {
		out := make([]Task, len(tasks))
		copy(out, tasks)
		return out
	}
	var out []Task
	for _, t := range tasks {
		if opt.tag != "" {
			hit := false
			for _, tg := range t.Tags {
				if strings.EqualFold(tg, opt.tag) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
		}
		if opt.status != "" && t.Status != opt.status {
			continue
		}
		out = append(out, t)
	}
	return out
}

func statusText(s string) string {
	switch s {
	case "in_progress":
		return "进行中"
	case "done":
		return "已完成"
	default:
		return "待办"
	}
}

// cmdList 列出全部任务（按优先级排序，可经 opt 过滤），返回渲染后的多行文本。
func cmdList(opt listOptions) (string, error) {
	// status 过滤值做白名单校验：写错时明确报错，
	// 否则会静默返回"没有匹配的任务"，让人以为数据没了。
	if opt.status != "" && !validStatus(opt.status) {
		return "", fmt.Errorf("状态应为 todo|in_progress|done，收到 %q", opt.status)
	}
	all, err := listTasks()
	if err != nil {
		return "", err
	}
	tasks := filterTasks(all, opt)
	if len(tasks) == 0 {
		return "没有匹配的任务", nil
	}
	var sb strings.Builder
	for _, t := range tasks {
		mark := "[ ]"
		if t.Status == "done" {
			mark = "[x]"
		} else if t.Status == "in_progress" {
			mark = "[~]"
		}
		dueStr := ""
		if t.DueDate != "" {
			dueStr = "  截止:" + t.DueDate
			if t.isOverdue() {
				dueStr += "  ⚠逾期"
			}
		}
		tagStr := ""
		if len(t.Tags) > 0 {
			tagStr = "  #" + strings.Join(t.Tags, " #")
		}
		noteStr := ""
		if t.Note != "" {
			noteStr = "  备注:" + t.Note
		}
		fmt.Fprintf(&sb, "%s #%d [%s/%s] %s%s%s%s\n", mark, t.ID, priorityText(t.Priority), statusText(t.Status), t.Title, tagStr, dueStr, noteStr)
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// validStatus 判断状态值是否合法。
func validStatus(s string) bool {
	return s == "todo" || s == "in_progress" || s == "done"
}

// cmdSearch 按标题搜索，返回渲染后的多行文本（无命中含提示）。
func cmdSearch(q string) (string, error) {
	tasks, err := load()
	if err != nil {
		return "", err
	}
	q = strings.ToLower(q)
	var sb strings.Builder
	hits := 0
	for _, t := range tasks {
		if strings.Contains(strings.ToLower(t.Title), q) {
			mark := "[ ]"
			if t.Status == "done" {
				mark = "[x]"
			} else if t.Status == "in_progress" {
				mark = "[~]"
			}
			fmt.Fprintf(&sb, "%s #%d %s\n", mark, t.ID, t.Title)
			hits++
		}
	}
	if hits == 0 {
		return "没找到匹配: " + q, nil
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// cmdDone 标记某条任务为完成，返回结果文案或错误（编号不存在时）。
func cmdDone(id int) (string, error) {
	return setStatus(id, "done")
}

// cmdStart 把任务置为进行中。
func cmdStart(id int) (string, error) {
	return setStatus(id, "in_progress")
}

func setStatus(id int, status string) (string, error) {
	if !validStatus(status) {
		return "", fmt.Errorf("非法状态: %q", status)
	}

	storeMu.Lock()
	defer storeMu.Unlock()

	tasks, err := load()
	if err != nil {
		return "", err
	}
	found := false
	for i := range tasks {
		if tasks[i].ID == id {
			tasks[i].Status = status
			found = true
			// 只改第一条匹配的，与 cmdRm 的行为保持一致。
			break
		}
	}
	if !found {
		return "", fmt.Errorf("没找到编号: %d", id)
	}
	if err := save(tasks); err != nil {
		return "", err
	}
	verb := "完成"
	if status == "in_progress" {
		verb = "进行中"
	}
	return fmt.Sprintf("已标记%s #%d", verb, id), nil
}

// cmdRm 删除某条任务，返回结果文案或错误（编号不存在时）。
func cmdRm(id int) (string, error) {
	storeMu.Lock()
	defer storeMu.Unlock()

	tasks, err := load()
	if err != nil {
		return "", err
	}
	idx := -1
	for i := range tasks {
		if tasks[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return "", fmt.Errorf("没找到编号: %d", id)
	}
	tasks = append(tasks[:idx], tasks[idx+1:]...)
	if err := save(tasks); err != nil {
		return "", err
	}
	return fmt.Sprintf("已删除 #%d", id), nil
}

func usage() {
	fmt.Print(`go-todo 本地待办看板

用法:
  go-todo add <标题> [-tag a,b] [-pri high|mid|low] [-due 2026-08-10] [-note 备注]   加一条任务
  go-todo list [-tag 工作] [-status todo|in_progress|done]   列出任务（可按标签/状态过滤）
  go-todo search <关键词>          按标题搜索
  go-todo start <编号>            标记为进行中
  go-todo done <编号>             标记为完成
  go-todo rm <编号>               删除任务
  go-todo -mode web               启动网页界面（默认 http://localhost:8080）
`)
}

func main() {
	// -mode 决定运行模式：cli=命令行（默认），web=网页界面。
	mode := flag.String("mode", "cli", "运行模式：cli=命令行, web=网页界面")
	addr := flag.String("addr", ":8080", "Web 监听地址（仅 -mode web 生效）")
	flag.Parse()

	if *mode == "web" {
		if err := StartServer(*addr); err != nil {
			fmt.Fprintf(os.Stderr, "启动 Web 服务失败: %v\n", err)
			os.Exit(1)
		}
		return
	}

	args := flag.Args()
	if len(args) < 1 {
		usage()
		return
	}
	switch args[0] {
	case "add":
		out, err := cmdAdd(args[1:])
		if err != nil {
			fail(err)
		}
		fmt.Println(out)
	case "list":
		opt := listOptions{}
		for i := 1; i < len(args); i++ {
			switch args[i] {
			case "-tag":
				if i+1 < len(args) {
					opt.tag = strings.TrimSpace(args[i+1])
					i++
				}
			case "-status":
				if i+1 < len(args) {
					opt.status = strings.TrimSpace(args[i+1])
					i++
				}
			}
		}
		out, err := cmdList(opt)
		if err != nil {
			fail(err)
		}
		fmt.Println(out)
	case "search":
		if len(args) < 2 {
			fmt.Println("用法: go-todo search <关键词>")
			return
		}
		out, err := cmdSearch(strings.Join(args[1:], " "))
		if err != nil {
			fail(err)
		}
		fmt.Println(out)
	case "start":
		runByID(args, "start", cmdStart)
	case "done":
		runByID(args, "done", cmdDone)
	case "rm":
		runByID(args, "rm", cmdRm)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintln(os.Stderr, "未知命令:", args[0])
		usage()
		os.Exit(2)
	}
}

// runByID 收敛 start/done/rm 三条命令重复的"取编号—执行—打印"流程。
func runByID(args []string, name string, fn func(int) (string, error)) {
	if len(args) < 2 {
		fmt.Fprintf(os.Stderr, "用法: go-todo %s <编号>\n", name)
		os.Exit(2)
	}
	id, err := parseID(args[1])
	if err != nil {
		fail(err)
	}
	out, err := fn(id)
	if err != nil {
		fail(err)
	}
	fmt.Println(out)
}

// fail 统一把错误写到 stderr 并以非零码退出。
// 原来出错时只 fmt.Println 到 stdout 且退出码为 0，
// 脚本里 `go-todo done 999 && echo ok` 会误判成功。
func fail(err error) {
	fmt.Fprintln(os.Stderr, "错误:", err)
	os.Exit(1)
}
