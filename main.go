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
	"sort"
	"strings"
	"time"
)

// Task 是一条待办任务。
type Task struct {
	ID        int      `json:"id"`
	Title     string   `json:"title"`
	Tags      []string `json:"tags"`
	Priority  int      `json:"priority"`  // 1=低 2=中 3=高
	Status    string   `json:"status"`    // todo / in_progress / done
	DueDate   string   `json:"due_date"`  // 截止日期，空表示无
	Note      string   `json:"note"`      // 备注，空表示无
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

// loadFromFile 读取指定路径的任务；文件不存在时返回空切片而不是报错。
func loadFromFile(path string) []Task {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []Task{}
		}
		fmt.Println("读取数据失败:", err)
		os.Exit(1)
	}
	if len(b) == 0 {
		return []Task{}
	}
	var tasks []Task
	if err := json.Unmarshal(b, &tasks); err != nil {
		fmt.Println("数据文件损坏:", err)
		os.Exit(1)
	}
	return tasks
}

func load() []Task { return loadFromFile(dataFile) }

// saveToFile 把任务写到指定路径，带缩进方便人直接看。
func saveToFile(tasks []Task, path string) {
	b, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		fmt.Println("序列化失败:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fmt.Println("写入失败:", err)
		os.Exit(1)
	}
}

func save(tasks []Task) { saveToFile(tasks, dataFile) }

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
	title := strings.Join(rest, " ")
	tasks := load()
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
	save(tasks)
	return fmt.Sprintf("已添加 #%d：%s", t.ID, t.Title), nil
}

// listTasks 返回当前所有任务（按优先级降序、ID 升序），供展示与测试复用。
func listTasks() []Task {
	tasks := load()
	sort.SliceStable(tasks, func(i, j int) bool {
		if tasks[i].Priority != tasks[j].Priority {
			return tasks[i].Priority > tasks[j].Priority
		}
		return tasks[i].ID < tasks[j].ID
	})
	return tasks
}

// listOptions 是 list 的过滤选项。
type listOptions struct {
	tag    string // 只显示含该标签的任务（空=不过滤）
	status string // 只显示该状态的任务（空=不过滤）
}

// filterTasks 按选项过滤任务；tag/status 为空表示不过滤。
func filterTasks(tasks []Task, opt listOptions) []Task {
	if opt.tag == "" && opt.status == "" {
		return tasks
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
func cmdList(opt listOptions) string {
	tasks := filterTasks(listTasks(), opt)
	if len(tasks) == 0 {
		return "没有匹配的任务"
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
	return strings.TrimRight(sb.String(), "\n")
}

// cmdSearch 按标题搜索，返回渲染后的多行文本（无命中含提示）。
func cmdSearch(q string) string {
	tasks := load()
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
		return "没找到匹配: " + q
	}
	return strings.TrimRight(sb.String(), "\n")
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
	tasks := load()
	found := false
	for i := range tasks {
		if tasks[i].ID == id {
			tasks[i].Status = status
			found = true
		}
	}
	if !found {
		return "", fmt.Errorf("没找到编号: %d", id)
	}
	save(tasks)
	verb := "完成"
	if status == "in_progress" {
		verb = "进行中"
	}
	return fmt.Sprintf("已标记%s #%d", verb, id), nil
}

// cmdRm 删除某条任务，返回结果文案或错误（编号不存在时）。
func cmdRm(id int) (string, error) {
	tasks := load()
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
	save(tasks)
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
			fmt.Println(err)
			return
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
		fmt.Println(cmdList(opt))
	case "search":
		if len(args) < 2 {
			fmt.Println("用法: go-todo search <关键词>")
			return
		}
		fmt.Println(cmdSearch(strings.Join(args[1:], " ")))
	case "start":
		if len(args) < 2 {
			fmt.Println("用法: go-todo start <编号>")
			return
		}
		var id int
		if _, err := fmt.Sscanf(args[1], "%d", &id); err != nil {
			fmt.Println("编号需要是数字")
			return
		}
		out, err := cmdStart(id)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(out)
	case "done":
		if len(args) < 2 {
			fmt.Println("用法: go-todo done <编号>")
			return
		}
		var id int
		if _, err := fmt.Sscanf(args[1], "%d", &id); err != nil {
			fmt.Println("编号需要是数字")
			return
		}
		out, err := cmdDone(id)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(out)
	case "rm":
		if len(args) < 2 {
			fmt.Println("用法: go-todo rm <编号>")
			return
		}
		var id int
		if _, err := fmt.Sscanf(args[1], "%d", &id); err != nil {
			fmt.Println("编号需要是数字")
			return
		}
		out, err := cmdRm(id)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(out)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Println("未知命令:", args[0])
		usage()
	}
}
