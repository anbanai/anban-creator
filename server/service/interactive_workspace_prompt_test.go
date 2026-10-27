package service

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceRuntimePreservesFirstMessage(t *testing.T) {
	for _, intent := range []string{"create-project", "update-project-profile", ""} {
		for _, timing := range []string{"before-start", "after-start"} {
			t.Run(intent+"/"+timing, func(t *testing.T) {
				rt := &interactiveRecordingRuntime{}
				svc := NewInteractiveServiceWithRuntime(nil, interactiveRecordingRuntimeFactory{runtime: rt})
				sess := &InteractiveSession{ID: "profile-session", UserID: "user-1", WorkspaceID: "workspace-1", Intent: intent, ExpiresAt: time.Now().Add(time.Hour), runtime: rt, watchers: map[*interactiveWatcher]struct{}{}}
				svc.sessions[sess.ID] = sess
				first := strings.Repeat("我是茶厂负责人，面向茶叶店主，希望分享选茶经验，不承诺收益。", 200)
				if timing == "after-start" {
					svc.initializeRuntimeSession(context.Background(), sess, rt)
				}
				if err := svc.Message(context.Background(), sess.UserID, sess.ID, first); err != nil {
					t.Fatalf("first Message(): %v", err)
				}
				if timing == "before-start" {
					svc.initializeRuntimeSession(context.Background(), sess, rt)
				}
				rt.mu.Lock()
				methods := append([]string(nil), rt.methods...)
				args := append([]any(nil), rt.args...)
				rt.mu.Unlock()
				if !reflect.DeepEqual(methods, []string{"session/start", "session/prompt"}) {
					t.Fatalf("commands = %v, want start then exactly one user prompt", methods)
				}
				prompt := args[1].(map[string]any)
				if !reflect.DeepEqual(prompt["contentBlocks"], []map[string]string{{"type": "text", "text": first}}) {
					t.Fatal("first user message was replaced or truncated")
				}
				sess.mu.Lock()
				queued, busy := sess.queuedPrompt, sess.busy
				sess.mu.Unlock()
				if queued != "" || !busy {
					t.Fatalf("queued bytes=%d busy=%v; expected consumed prompt and active turn", len(queued), busy)
				}
				svc.runtimeEvent(sess.ID, "turn.end", map[string]any{"session_id": sess.ID})
				if err := svc.Message(context.Background(), sess.UserID, sess.ID, "我刚才说的受众是谁？"); err != nil {
					t.Fatalf("follow-up Message(): %v", err)
				}
				rt.mu.Lock()
				defer rt.mu.Unlock()
				if len(rt.methods) != 3 || rt.methods[2] != "session/prompt" || rt.args[2].(map[string]any)["sessionId"] != sess.ID {
					t.Fatal("follow-up did not reuse the same conversation")
				}
			})
		}
	}
}
