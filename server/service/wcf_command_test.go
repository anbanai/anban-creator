package service

import (
	"testing"
	"time"

	"github.com/anbanai/anban-creator/server/model"
)

func TestParseCommand(t *testing.T) {
	cases := []struct {
		name     string
		input    string
		wantKind wcfCommandKind
		wantArgs string
	}{
		// Help / unknown
		{"empty", "", cmdUnknown, ""},
		{"spaces only", "   ", cmdUnknown, ""},
		{"help cn", "帮助", cmdHelp, ""},
		{"help q half", "?", cmdHelp, ""},
		{"help q full", "？", cmdHelp, ""},
		{"help en", "help", cmdHelp, ""},
		{"help padded", "  帮助  ", cmdHelp, ""},
		{"unknown", "随便说一句", cmdUnknown, ""},

		// Recent
		{"recent cn", "最近", cmdRecent, ""},
		{"recent list", "list", cmdRecent, ""},
		{"recent recent", "recent", cmdRecent, ""},

		// Status (id-taking)
		{"status spaced", "状态 abc-123", cmdStatus, "abc-123"},
		{"status nospaced", "查询abc-123", cmdStatus, "abc-123"},
		{"status bare", "状态", cmdStatus, ""},
		{"status en", "status 456", cmdStatus, "456"},

		// Cancel
		{"cancel spaced", "取消 abc-123", cmdCancel, "abc-123"},
		{"cancel nospaced", "取消abc-123", cmdCancel, "abc-123"},

		// Create — task TYPE comes from the project; these are pure aliases.
		// Only explicit genre verbs trigger; bare high-frequency words (写/文章/任务)
		// are excluded so casual phrases don't create billable tasks.
		{"create article spaced", "写文章 夏日防晒指南", cmdCreate, "夏日防晒指南"},
		{"create article nospaced", "写文章夏日防晒", cmdCreate, "夏日防晒"},
		{"create seednote", "种草 测评好物", cmdCreate, "测评好物"},
		{"create seednote verb", "写种草新品", cmdCreate, "新品"},
		{"create poster", "海报 双十一促销", cmdCreate, "双十一促销"},
		{"create generic verb", "创建 新主题", cmdCreate, "新主题"},
		{"create generic verb bare", "创建", cmdCreate, ""},
		// Bare/ambiguous prefixes are NOT create triggers (anti-false-positive).
		{"casual write not create", "写 测试主题", cmdUnknown, ""},
		{"bare write not create", "写", cmdUnknown, ""},
		{"casual article not create", "文章写得不错", cmdUnknown, ""},
		{"casual task not create", "任务太多了", cmdUnknown, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCommand(tc.input)
			if got.kind != tc.wantKind {
				t.Fatalf("parseCommand(%q).kind = %v, want %v", tc.input, got.kind, tc.wantKind)
			}
			if got.args != tc.wantArgs {
				t.Fatalf("parseCommand(%q).args = %q, want %q", tc.input, got.args, tc.wantArgs)
			}
		})
	}
}

func TestTaskTypeLabel(t *testing.T) {
	cases := map[string]string{
		model.PlatformArticle:   "文章",
		model.PlatformSeednote:  "种草笔记",
		model.PlatformEcommerce: "电商图",
		"poster":                "海报",
		"":                      "任务",
		"weird":                 "weird",
	}
	for in, want := range cases {
		if got := taskTypeLabel(in); got != want {
			t.Errorf("taskTypeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTaskStatusLabel(t *testing.T) {
	cases := map[string]string{
		model.TaskStatusPending:   "排队中",
		model.TaskStatusRunning:   "执行中",
		model.TaskStatusCompleted: "已完成",
		model.TaskStatusFailed:    "失败",
		model.TaskStatusCancelled: "已取消",
		"":                        "未知",
	}
	for in, want := range cases {
		if got := taskStatusLabel(in); got != want {
			t.Errorf("taskStatusLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("短文本", 10); got != "短文本" {
		t.Errorf("truncateRunes short = %q, want %q", got, "短文本")
	}
	// 5 runes, limit 3 → first 3 + ellipsis
	if got := truncateRunes("一二三四五", 3); got != "一二三…" {
		t.Errorf("truncateRunes long = %q, want %q", got, "一二三…")
	}
}

func TestCleanErr(t *testing.T) {
	if got := cleanErr("  spaced  "); got != "spaced" {
		t.Errorf("cleanErr trim = %q", got)
	}
	long := make([]rune, 200)
	for i := range long {
		long[i] = '字'
	}
	got := cleanErr(string(long))
	if len([]rune(got)) != 121 { // 120 + ellipsis
		t.Errorf("cleanErr long rune count = %d, want 121", len([]rune(got)))
	}
}

func TestWCFRateLimiter(t *testing.T) {
	l := newWCFRateLimiter(3, time.Minute)
	base := time.Unix(1000, 0)

	// First 3 within the window pass.
	for i := 0; i < 3; i++ {
		if !l.Allow("u1", base.Add(time.Duration(i)*time.Second)) {
			t.Fatalf("Allow #%d should pass", i+1)
		}
	}
	// 4th in the same window is rejected.
	if l.Allow("u1", base.Add(3*time.Second)) {
		t.Fatal("4th Allow within window should be rejected")
	}
	// A different user has its own bucket.
	if !l.Allow("u2", base) {
		t.Fatal("u2 should pass independently")
	}
	// After the window rolls past, the bucket resets.
	if !l.Allow("u1", base.Add(time.Minute+time.Second)) {
		t.Fatal("Allow after window expiry should pass")
	}
}
