package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func withTempData(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	tmp := filepath.Join(dir, "todo.json")
	old := dataFile
	dataFile = tmp
	t.Cleanup(func() { dataFile = old })
}

// mustAdd 加一条任务，失败直接终止用例，省去每处重复 if err。
func mustAdd(t *testing.T, args ...string) string {
	t.Helper()
	out, err := cmdAdd(args)
	if err != nil {
		t.Fatalf("cmdAdd(%v) 报错: %v", args, err)
	}
	return out
}

func mustList(t *testing.T, opt listOptions) string {
	t.Helper()
	out, err := cmdList(opt)
	if err != nil {
		t.Fatalf("cmdList(%+v) 报错: %v", opt, err)
	}
	return out
}

func mustLoad(t *testing.T) []Task {
	t.Helper()
	tasks, err := load()
	if err != nil {
		t.Fatalf("load 报错: %v", err)
	}
	return tasks
}

func TestParseTags(t *testing.T) {
	if got := parseTags("a,b c"); len(got) != 3 {
		t.Fatalf("parseTags 应拆成 3 个, got %v", got)
	}
	if got := parseTags(""); got != nil {
		t.Fatalf("空输入应返回 nil, got %v", got)
	}
}

func TestParsePriority(t *testing.T) {
	cases := map[string]int{"high": 3, "mid": 2, "low": 1, "h": 3, "2": 2, "": 2, "9": 3, "0": 1}
	for in, want := range cases {
		if got := parsePriority(in); got != want {
			t.Fatalf("parsePriority(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestNextID(t *testing.T) {
	if got := nextID(nil); got != 1 {
		t.Fatalf("nextID(nil) = %d, want 1", got)
	}
	if got := nextID([]Task{{ID: 3}, {ID: 9}}); got != 10 {
		t.Fatalf("nextID = %d, want 10", got)
	}
}

func TestCmdAddAndList(t *testing.T) {
	withTempData(t)
	out := mustAdd(t, "写周报", "-tag", "工作", "-pri", "high", "-due", "2026-08-10")
	if !strings.Contains(out, "#1") {
		t.Fatalf("cmdAdd 输出异常: %q", out)
	}
	mustAdd(t, "低优先级的事", "-pri", "low")
	listed := mustList(t, listOptions{})
	// 高优先级应排在前面
	hi := strings.Index(listed, "写周报")
	lo := strings.Index(listed, "低优先级的事")
	if hi < 0 || lo < 0 || hi > lo {
		t.Fatalf("高优先级应排在前面:\n%s", listed)
	}
	raw, _ := os.ReadFile(dataFile)
	if !strings.Contains(string(raw), "写周报") || !strings.Contains(string(raw), "2026-08-10") {
		t.Fatalf("数据未正确落盘: %s", string(raw))
	}
}

func TestCmdAddUsageError(t *testing.T) {
	withTempData(t)
	if _, err := cmdAdd(nil); err == nil {
		t.Fatal("空参数应报错")
	}
	if _, err := cmdAdd([]string{"-tag", "x"}); err == nil {
		t.Fatal("只有 -tag 无标题应报错")
	}
}

func TestCmdStatusAndRm(t *testing.T) {
	withTempData(t)
	mustAdd(t, "任务A")
	mustAdd(t, "任务B")
	if _, err := cmdStart(1); err != nil {
		t.Fatalf("cmdStart 报错: %v", err)
	}
	if _, err := cmdDone(2); err != nil {
		t.Fatalf("cmdDone 报错: %v", err)
	}
	tasks := mustLoad(t)
	if tasks[0].Status != "in_progress" {
		t.Fatalf("任务1 应为进行中, got %s", tasks[0].Status)
	}
	if tasks[1].Status != "done" {
		t.Fatalf("任务2 应为完成, got %s", tasks[1].Status)
	}
	if _, err := cmdRm(1); err != nil {
		t.Fatalf("cmdRm 报错: %v", err)
	}
	if len(mustLoad(t)) != 1 {
		t.Fatal("删除后应只剩 1 条")
	}
	if _, err := cmdRm(999); err == nil {
		t.Fatal("删除不存在的编号应报错")
	}
	if _, err := cmdStart(999); err == nil {
		t.Fatal("标记不存在的编号应报错")
	}
}

func TestCmdSearch(t *testing.T) {
	withTempData(t)
	mustAdd(t, "学习 Go 语言")
	mustAdd(t, "去超市买菜")
	got, err := cmdSearch("GO")
	if err != nil {
		t.Fatalf("cmdSearch 报错: %v", err)
	}
	if !strings.Contains(got, "学习 Go 语言") || strings.Contains(got, "买菜") {
		t.Fatalf("搜索结果异常:\n%s", got)
	}
	miss, err := cmdSearch("不存在")
	if err != nil {
		t.Fatalf("cmdSearch 报错: %v", err)
	}
	if strings.Contains(miss, "学习") {
		t.Fatal("未命中不应出现内容")
	}
}

func TestCmdAddNote(t *testing.T) {
	withTempData(t)
	out := mustAdd(t, "写报告", "-note", "周五前交给老板")
	if !strings.Contains(out, "#1") {
		t.Fatalf("cmdAdd 输出异常: %q", out)
	}
	listed := mustList(t, listOptions{})
	if !strings.Contains(listed, "周五前交给老板") {
		t.Fatalf("备注未显示:\n%s", listed)
	}
	raw, _ := os.ReadFile(dataFile)
	if !strings.Contains(string(raw), "周五前交给老板") {
		t.Fatalf("备注未落盘: %s", string(raw))
	}
}

func TestListFilter(t *testing.T) {
	withTempData(t)
	mustAdd(t, "任务A", "-tag", "工作", "-pri", "high")
	mustAdd(t, "任务B", "-tag", "生活")
	mustAdd(t, "任务C", "-tag", "工作", "-pri", "low")

	// 按标签过滤
	if got := mustList(t, listOptions{tag: "工作"}); !strings.Contains(got, "任务A") || !strings.Contains(got, "任务C") || strings.Contains(got, "任务B") {
		t.Fatalf("按标签过滤异常:\n%s", got)
	}
	// 按状态过滤（全为待办）
	if got := mustList(t, listOptions{status: "done"}); got != "没有匹配的任务" {
		t.Fatalf("按状态过滤应无结果, got:\n%s", got)
	}
	if _, err := cmdDone(1); err != nil {
		t.Fatalf("cmdDone 报错: %v", err)
	}
	if got := mustList(t, listOptions{status: "done"}); !strings.Contains(got, "任务A") {
		t.Fatalf("完成状态过滤异常:\n%s", got)
	}
	if got := mustList(t, listOptions{tag: "工作", status: "done"}); !strings.Contains(got, "任务A") || strings.Contains(got, "任务C") {
		t.Fatalf("标签+状态组合过滤异常:\n%s", got)
	}
}

// 拼错状态值以前会静默返回"没有匹配的任务"，让人以为数据丢了；现在必须报错。
func TestCmdListRejectsBadStatus(t *testing.T) {
	withTempData(t)
	mustAdd(t, "任务A")
	if _, err := cmdList(listOptions{status: "DONE"}); err == nil {
		t.Fatal("非法状态应报错，而不是返回空列表")
	}
	if _, err := cmdList(listOptions{status: "finished"}); err == nil {
		t.Fatal("非法状态应报错")
	}
}

func TestValidateDue(t *testing.T) {
	ok := []string{"2026-08-10", "2000-01-01", "2026-02-28"}
	for _, s := range ok {
		if err := validateDue(s); err != nil {
			t.Fatalf("validateDue(%q) 不应报错: %v", s, err)
		}
	}
	// 这些以前会被原样存下来，然后让 isOverdue 的字符串比较得出荒谬结论：
	// "9999" 永不逾期，"8/10/2026" 永远逾期。
	bad := []string{"9999", "2026/08/10", "8/10/2026", "2026-13-01", "2026-02-30", "明天", ""}
	for _, s := range bad {
		if err := validateDue(s); err == nil {
			t.Fatalf("validateDue(%q) 应报错", s)
		}
	}
}

func TestCmdAddRejectsBadDue(t *testing.T) {
	withTempData(t)
	if _, err := cmdAdd([]string{"任务", "-due", "9999"}); err == nil {
		t.Fatal("非法截止日应被拒绝")
	}
	if len(mustLoad(t)) != 0 {
		t.Fatal("被拒绝的任务不应落盘")
	}
}

func TestParseID(t *testing.T) {
	if id, err := parseID(" 12 "); err != nil || id != 12 {
		t.Fatalf("parseID(\" 12 \") = %d, %v", id, err)
	}
	// fmt.Sscanf("%d") 会把 "12abc" 静默解析成 12，strconv.Atoi 不会。
	bad := []string{"12abc", "abc", "", "0", "-3", "1.5", "١٢"}
	for _, s := range bad {
		if _, err := parseID(s); err == nil {
			t.Fatalf("parseID(%q) 应报错", s)
		}
	}
}

func TestValidStatus(t *testing.T) {
	for _, s := range []string{"todo", "in_progress", "done"} {
		if !validStatus(s) {
			t.Fatalf("%q 应为合法状态", s)
		}
	}
	for _, s := range []string{"", "DONE", "doing", "todo "} {
		if validStatus(s) {
			t.Fatalf("%q 不应为合法状态", s)
		}
	}
}

func TestSetStatusRejectsBadStatus(t *testing.T) {
	withTempData(t)
	mustAdd(t, "任务A")
	if _, err := setStatus(1, "deleted"); err == nil {
		t.Fatal("非法状态应被拒绝")
	}
	if mustLoad(t)[0].Status != "todo" {
		t.Fatal("非法状态不应改动数据")
	}
}

// filterTasks 不过滤时以前直接返回入参切片，调用方 append 会踩到同一块底层数组。
func TestFilterTasksReturnsCopy(t *testing.T) {
	src := []Task{{ID: 1, Title: "原始"}}
	got := filterTasks(src, listOptions{})
	got[0].Title = "被改了"
	if src[0].Title != "原始" {
		t.Fatal("filterTasks 不应让调用方改到原切片")
	}
}

func TestLoadCorruptedFile(t *testing.T) {
	withTempData(t)
	if err := os.WriteFile(dataFile, []byte("{ 这不是合法 JSON"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := load(); err == nil {
		t.Fatal("损坏的数据文件应返回错误，而不是当成空列表把数据覆盖掉")
	}
}

func TestLoadMissingAndEmptyFile(t *testing.T) {
	withTempData(t)
	// 文件不存在
	tasks, err := load()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("文件不存在应返回空列表, got %v, %v", tasks, err)
	}
	// 空文件（比如上次写盘写了个 0 字节）
	if err := os.WriteFile(dataFile, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tasks, err = load()
	if err != nil || len(tasks) != 0 {
		t.Fatalf("空文件应返回空列表, got %v, %v", tasks, err)
	}
}

// saveToFile 用临时文件 + Rename，写完不该在目录里留下 .todo-*.tmp。
func TestSaveIsAtomicAndLeavesNoTemp(t *testing.T) {
	withTempData(t)
	if err := save([]Task{{ID: 1, Title: "A"}}); err != nil {
		t.Fatalf("save 报错: %v", err)
	}
	// 覆盖写一次，确认旧内容被完整替换而不是叠加。
	if err := save([]Task{{ID: 2, Title: "B"}}); err != nil {
		t.Fatalf("save 报错: %v", err)
	}
	tasks := mustLoad(t)
	if len(tasks) != 1 || tasks[0].ID != 2 {
		t.Fatalf("覆盖写异常: %+v", tasks)
	}

	entries, err := os.ReadDir(filepath.Dir(dataFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".todo-") {
			t.Fatalf("残留临时文件: %s", e.Name())
		}
	}
}

func TestSaveToUnwritableDir(t *testing.T) {
	withTempData(t)
	old := dataFile
	dataFile = filepath.Join(old, "不存在的子目录", "todo.json")
	t.Cleanup(func() { dataFile = old })
	if err := save([]Task{{ID: 1}}); err == nil {
		t.Fatal("写到不存在的目录应返回错误")
	}
}

// 并发 add：没有锁时两个 goroutine 会读到同样的数据、算出同一个 ID，
// 后写的把先写的整个覆盖掉。加锁后 N 次 add 必须得到 N 条且 ID 互不重复。
// 配合 -race 还能验证没有数据竞争。
func TestConcurrentAddNoLostUpdate(t *testing.T) {
	withTempData(t)
	const n = 20
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = cmdAdd([]string{"并发任务"})
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 次 cmdAdd 报错: %v", i, err)
		}
	}
	tasks := mustLoad(t)
	if len(tasks) != n {
		t.Fatalf("并发 add 后应有 %d 条, got %d（丢更新）", n, len(tasks))
	}
	seen := map[int]bool{}
	for _, tk := range tasks {
		if seen[tk.ID] {
			t.Fatalf("ID 重复: %d", tk.ID)
		}
		seen[tk.ID] = true
	}
}

func TestIsOverdue(t *testing.T) {
	today := todayStr()
	past := "2000-01-01"
	future := "2999-12-31"
	cases := []struct {
		name string
		task Task
		want bool
	}{
		{"过去未完成", Task{DueDate: past, Status: "todo"}, true},
		{"过去已完成", Task{DueDate: past, Status: "done"}, false},
		{"将来未完成", Task{DueDate: future, Status: "todo"}, false},
		{"无截止日", Task{Status: "todo"}, false},
		{"今天", Task{DueDate: today, Status: "todo"}, false},
	}
	for _, c := range cases {
		if got := c.task.isOverdue(); got != c.want {
			t.Fatalf("%s: isOverdue=%v, want %v", c.name, got, c.want)
		}
	}
}
