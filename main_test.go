package main

import (
	"os"
	"path/filepath"
	"strings"
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
	out, err := cmdAdd([]string{"写周报", "-tag", "工作", "-pri", "high", "-due", "2026-08-10"})
	if err != nil {
		t.Fatalf("cmdAdd 报错: %v", err)
	}
	if !strings.Contains(out, "#1") {
		t.Fatalf("cmdAdd 输出异常: %q", out)
	}
	cmdAdd([]string{"低优先级的事", "-pri", "low"})
	listed := cmdList(listOptions{})
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
	cmdAdd([]string{"任务A"})
	cmdAdd([]string{"任务B"})
	if _, err := cmdStart(1); err != nil {
		t.Fatalf("cmdStart 报错: %v", err)
	}
	if _, err := cmdDone(2); err != nil {
		t.Fatalf("cmdDone 报错: %v", err)
	}
	tasks := load()
	if tasks[0].Status != "in_progress" {
		t.Fatalf("任务1 应为进行中, got %s", tasks[0].Status)
	}
	if tasks[1].Status != "done" {
		t.Fatalf("任务2 应为完成, got %s", tasks[1].Status)
	}
	if _, err := cmdRm(1); err != nil {
		t.Fatalf("cmdRm 报错: %v", err)
	}
	if len(load()) != 1 {
		t.Fatal("删除后应只剩 1 条")
	}
	if _, err := cmdRm(999); err == nil {
		t.Fatal("删除不存在的编号应报错")
	}
}

func TestCmdSearch(t *testing.T) {
	withTempData(t)
	cmdAdd([]string{"学习 Go 语言"})
	cmdAdd([]string{"去超市买菜"})
	got := cmdSearch("GO")
	if !strings.Contains(got, "学习 Go 语言") || strings.Contains(got, "买菜") {
		t.Fatalf("搜索结果异常:\n%s", got)
	}
	if strings.Contains(cmdSearch("不存在"), "学习") {
		t.Fatal("未命中不应出现内容")
	}
}

func TestCmdAddNote(t *testing.T) {
	withTempData(t)
	out, err := cmdAdd([]string{"写报告", "-note", "周五前交给老板"})
	if err != nil {
		t.Fatalf("cmdAdd 报错: %v", err)
	}
	if !strings.Contains(out, "#1") {
		t.Fatalf("cmdAdd 输出异常: %q", out)
	}
	listed := cmdList(listOptions{})
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
	cmdAdd([]string{"任务A", "-tag", "工作", "-pri", "high"})
	cmdAdd([]string{"任务B", "-tag", "生活"})
	cmdAdd([]string{"任务C", "-tag", "工作", "-pri", "low"})

	// 按标签过滤
	if got := cmdList(listOptions{tag: "工作"}); !strings.Contains(got, "任务A") || !strings.Contains(got, "任务C") || strings.Contains(got, "任务B") {
		t.Fatalf("按标签过滤异常:\n%s", got)
	}
	// 按状态过滤（全为待办）
	if got := cmdList(listOptions{status: "done"}); got != "没有匹配的任务" {
		t.Fatalf("按状态过滤应无结果, got:\n%s", got)
	}
	cmdDone(1)
	if got := cmdList(listOptions{status: "done"}); !strings.Contains(got, "任务A") {
		t.Fatalf("完成状态过滤异常:\n%s", got)
	}
	if got := cmdList(listOptions{tag: "工作", status: "done"}); !strings.Contains(got, "任务A") || strings.Contains(got, "任务C") {
		t.Fatalf("标签+状态组合过滤异常:\n%s", got)
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
