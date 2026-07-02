package mcp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLiveSliceSkillFiles(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))
	var firstBody string
	for _, plugin := range []string{"claudecode", "openclaw", "codex"} {
		skillDir := filepath.Join(root, plugin, "skills", "live-slice")
		skillPath := filepath.Join(skillDir, "SKILL.md")
		legacyPythonHelper := "live_slice_media" + ".py"
		scriptPath := filepath.Join(skillDir, "scripts", legacyPythonHelper)

		raw, err := os.ReadFile(skillPath)
		if err != nil {
			t.Fatalf("%s SKILL.md missing: %v", plugin, err)
		}
		body := string(raw)
		for _, want := range []string{
			"name: live-slice",
			"直播切片",
			"剪直播",
			"智能切片",
			"听悟",
			"live video slicing",
			"ffprobe -v error -show_format -show_streams -of json \"$VIDEO\" > \"$DIR/metadata.json\"",
			"ffmpeg -y -i \"$VIDEO\" -vn -ac 1 -ar 16000 -codec:a libmp3lame -q:a 4 \"$DIR/audio.mp3\"",
			"ffmpeg -y -ss 0 -i \"$VIDEO\" -frames:v 1 -q:v 2 \"$DIR/cover.jpg\"",
			"ffmpeg -y -ss \"$START\" -i \"$VIDEO\" -t \"$DURATION\" -c copy \"$OUT\"",
			"ffmpeg -y -ss \"$START\" -i \"$VIDEO\" -t \"$DURATION\" -c:v libx264 -c:a aac \"$OUT\"",
			"get_media_pipeline_status",
			"prepare_file_upload",
			`purpose="live_audio"`,
			"create_live_analysis_task(audio_key=",
			"analysis.json",
			"segments.json",
			"build_live_clip_plan",
			"build_live_clip_manifest",
			"build_live_subject_clip_plan",
			"clip-plan.json",
			"subject-clip-plan.json",
			"clip_results.json",
			"clip-manifest.json",
			"clip_notes_markdown",
			"markdown_path",
			"actual_duration_seconds",
			"concat_shell",
			"concat_list_content",
			"parent directories",
			"part_results` only for multi-part clips",
			"transcript",
			"JSON array",
		} {
			if !strings.Contains(body, want) {
				t.Fatalf("%s SKILL.md missing %q", plugin, want)
			}
		}
		for _, banned := range []string{"python" + "3", legacyPythonHelper, `DUR` + `ATION="$END_TIME-START_TIME"`, `DUR` + `ATION="$END_TIME` + ` - $START_TIME"`, "补充或替换 `segments" + ".json`", "Call `upload_live_audio"} {
			if strings.Contains(body, banned) {
				t.Fatalf("%s SKILL.md still mentions %q", plugin, banned)
			}
		}
		if _, err := os.Stat(scriptPath); !os.IsNotExist(err) {
			t.Fatalf("%s Python helper should not exist after pure ffmpeg migration", plugin)
		}
		if firstBody == "" {
			firstBody = body
		} else if body != firstBody {
			t.Fatalf("live-slice SKILL.md differs between plugins")
		}
	}
}

func TestLiveSlicerAgentFile(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))

	agentPath := filepath.Join(root, "claudecode", "agents", "live-slicer.md")
	raw, err := os.ReadFile(agentPath)
	if err != nil {
		t.Fatalf("live-slicer agent missing: %v", err)
	}
	body := string(raw)
	for _, want := range []string{
		"name: live-slicer",
		"直播切片",
		"mcpServers:",
		"- creator",
		"skills:",
		"- live-slice",
		"- TaskCreate",
		"- TaskUpdate",
		"- Read",
		"- Write",
		"- Bash",
		"get_media_pipeline_status",
		"prepare_file_upload",
		`purpose="live_audio"`,
		"create_live_analysis_task",
		"create_live_analysis_task",
		"query_live_analysis_task",
		"recognize_live_invalid_sentences",
		"recognize_live_segments",
		"build_live_clip_plan",
		"build_live_clip_manifest",
		"recognize_live_subjects",
		"complete_live_subject",
		"build_live_subject_clip_plan",
		"ffprobe",
		"ffmpeg",
		"maxTurns: 160",
		"metadata.json",
		"audio.mp3",
		"cover.jpg",
		"analysis.json",
		"invalid-sentences.json",
		"segments.json",
		"clip-plan.json",
		"subject-clip-plan.json",
		"clip_results.json",
		"clip_notes_markdown",
		"markdown_path",
		"actual_duration_seconds",
		"concat_shell",
		"concat_list_content",
		`mkdir -p "$(dirname "$OUT")"`,
		"单 part clip 不需要冗余 `part_results`",
		`ffprobe -v error -show_entries format=duration -of default=noprint_wrappers=1:nokey=1 "$OUT"`,
		"ffprobe",
		"duration",
		"需人工复核片段",
		"exports/",
		"clip-manifest.json",
		"summary.md",
		"$TINGWU_TASK_ID",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("live-slicer agent missing %q", want)
		}
	}
	for _, banned := range []string{
		"Python",
		"python" + "3",
		"live_slice_media" + ".py",
		"upload_live_audio",
		"自定义 HTTP 客户端",
		`DUR` + `ATION="$END_TIME` + ` - $START_TIME"`,
		"追加结构化记录",
		"为每个片段生成同名 `.md` 文本说明",
		"clip_notes_markdown[]." + "output",
		"输出不存在或输出为 0 字节，执行 `accurate_cut_shell`",
		"补充或替换 `segments" + ".json`",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("live-slicer agent should not mention %q", banned)
		}
	}

	hooksPath := filepath.Join(root, "claudecode", "hooks", "hooks.json")
	hooksRaw, err := os.ReadFile(hooksPath)
	if err != nil {
		t.Fatalf("claudecode hooks missing: %v", err)
	}
	hooks := string(hooksRaw)
	for _, want := range []string{
		`"matcher": "live-slicer"`,
		"output/live-slice/",
		"clip-manifest.json",
		"clip_results.json",
		"subject-clip-plan.json",
		"actual_duration_seconds",
		"warnings",
		"rejected",
		"exports/*.md",
		"需人工复核片段",
		"segments.json",
	} {
		if !strings.Contains(hooks, want) {
			t.Fatalf("claudecode hooks missing %q", want)
		}
	}

	claudePath := filepath.Join(root, "claudecode", "CLAUDE.md")
	claudeRaw, err := os.ReadFile(claudePath)
	if err != nil {
		t.Fatalf("claudecode CLAUDE.md missing: %v", err)
	}
	claude := string(claudeRaw)
	for _, want := range []string{
		"build_live_clip_plan",
		"build_live_clip_manifest",
		"build_live_subject_clip_plan",
		"recognize_live_subjects",
		"complete_live_subject",
	} {
		if !strings.Contains(claude, want) {
			t.Fatalf("claudecode CLAUDE.md missing %q", want)
		}
	}
}

func TestCapCutDraftSkillUsesShellJSONValidation(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", ".."))

	referencePath := filepath.Join(root, "claudecode", "skills", "capcut-draft", "references", "operations.md")
	raw, err := os.ReadFile(referencePath)
	if err != nil {
		t.Fatalf("capcut-draft operations reference missing: %v", err)
	}
	body := string(raw)

	for _, want := range []string{
		"jq empty \"<filePath>\"",
		"Valid JSON",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("capcut-draft operations reference missing %q", want)
		}
	}

	for _, banned := range []string{
		"python" + "3",
		"import json",
	} {
		if strings.Contains(body, banned) {
			t.Fatalf("capcut-draft operations reference should not mention %q", banned)
		}
	}
}
