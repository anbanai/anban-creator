package agent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

type archiveScriptResult struct {
	Status     string `json:"status"`
	Code       string `json:"code"`
	Message    string `json:"message"`
	ArchiveDir string `json:"archive_dir"`
}

func TestSeednoteArchiveScriptSyntaxAndMode(t *testing.T) {
	script := seednoteArchiveScriptPath(t)
	if output, err := exec.Command("bash", "-n", script).CombinedOutput(); err != nil {
		t.Fatalf("bash -n: %v\n%s", err, output)
	}
	info, err := os.Stat(script)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("archive script mode = %o, want executable", info.Mode().Perm())
	}
}

func TestSeednoteArchiveScriptUsesCandidateReservationsWithoutStaleLockRecovery(t *testing.T) {
	raw, err := os.ReadFile(seednoteArchiveScriptPath(t))
	if err != nil {
		t.Fatal(err)
	}
	script := string(raw)
	for _, want := range []string{".archive-reserve-", "RESERVATION_TOKEN", "STAGING_INODE", "archive_destination_race"} {
		if !strings.Contains(script, want) {
			t.Fatalf("archive script missing candidate-reservation contract %q", want)
		}
	}
	for _, forbidden := range []string{"LOCK_TTL", "RECOVERY_LOCK", ".stale-claim", "QUARANTINE", ".archive-lock-"} {
		if strings.Contains(script, forbidden) {
			t.Fatalf("archive script retained stale-lock recovery mechanism %q", forbidden)
		}
	}
}

func TestSeednoteArchiveScriptCopiesVerifiedTreeAndExcludesArchiveRoot(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "output")
	writeArchiveFixture(t, source, "first")
	oldArchive := filepath.Join(source, "seednote", "old")
	mustWriteArchiveFile(t, filepath.Join(oldArchive, "ignore.txt"), "old archive")
	proposed := filepath.Join(source, "seednote", "title")

	result, err := runSeednoteArchiveScript(t, source, proposed, nil)
	if err != nil {
		t.Fatalf("archive: %v (%+v)", err, result)
	}
	canonicalProposed := canonicalArchivePath(t, proposed)
	if result.Status != "archived" || result.ArchiveDir != canonicalProposed {
		t.Fatalf("result = %+v", result)
	}
	assertArchiveFixture(t, canonicalProposed, "first")
	if _, err := os.Stat(filepath.Join(canonicalProposed, "seednote", "old", "ignore.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive root was copied into result: %v", err)
	}
	assertArchiveFixture(t, source, "first")
	assertNoArchiveTemps(t, root)
}

func TestSeednoteArchiveScriptSelectsNextAvailableDestination(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeArchiveFixture(t, source, "new")
	proposed := filepath.Join(root, "archives", "title")
	mustWriteArchiveFile(t, filepath.Join(proposed, "sentinel.txt"), "existing")

	result, err := runSeednoteArchiveScript(t, source, proposed, nil)
	if err != nil {
		t.Fatalf("archive: %v (%+v)", err, result)
	}
	want := canonicalArchivePath(t, filepath.Join(root, "archives", "title-2"))
	if result.ArchiveDir != want {
		t.Fatalf("archive_dir = %q, want %q", result.ArchiveDir, want)
	}
	assertArchiveFixture(t, want, "new")
	data, err := os.ReadFile(filepath.Join(proposed, "sentinel.txt"))
	if err != nil || string(data) != "existing" {
		t.Fatalf("existing destination changed: %q, %v", data, err)
	}
	assertNoArchiveTemps(t, root)
}

func TestSeednoteArchiveScriptConcurrentRunsDoNotOverwrite(t *testing.T) {
	root := t.TempDir()
	proposed := filepath.Join(root, "archives", "title")
	sources := []string{filepath.Join(root, "source-a"), filepath.Join(root, "source-b")}
	writeArchiveFixture(t, sources[0], "alpha")
	writeArchiveFixture(t, sources[1], "beta")

	type outcome struct {
		result     archiveScriptResult
		commandErr error
		parseErr   error
	}
	outcomes := make([]outcome, len(sources))
	script := seednoteArchiveScriptPath(t)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range sources {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			outcomes[i].result, outcomes[i].commandErr, outcomes[i].parseErr = executeSeednoteArchiveScript(script, sources[i], proposed, nil)
		}(i)
	}
	close(start)
	wg.Wait()

	paths := make([]string, 0, len(outcomes))
	for i, outcome := range outcomes {
		if outcome.commandErr != nil || outcome.parseErr != nil {
			t.Fatalf("archive %d: command=%v parse=%v (%+v)", i, outcome.commandErr, outcome.parseErr, outcome.result)
		}
		paths = append(paths, outcome.result.ArchiveDir)
	}
	sort.Strings(paths)
	wantPaths := []string{
		canonicalArchivePath(t, filepath.Join(root, "archives", "title")),
		canonicalArchivePath(t, filepath.Join(root, "archives", "title-2")),
	}
	if strings.Join(paths, "\n") != strings.Join(wantPaths, "\n") {
		t.Fatalf("archive paths = %v, want %v", paths, wantPaths)
	}
	contents := []string{
		mustReadArchiveFile(t, filepath.Join(paths[0], "content.md")),
		mustReadArchiveFile(t, filepath.Join(paths[1], "content.md")),
	}
	sort.Strings(contents)
	if strings.Join(contents, ",") != "alpha,beta" {
		t.Fatalf("archive contents = %v", contents)
	}
	assertNoArchiveTemps(t, root)
}

func TestSeednoteArchiveScriptFailureIsRecoverable(t *testing.T) {
	tests := []struct {
		name     string
		envName  string
		toolName string
		wantCode string
	}{
		{name: "copy failure", envName: "ANBAN_ARCHIVE_TAR_BIN", toolName: "tar-fail", wantCode: "archive_copy_failed"},
		{name: "hash failure", envName: "ANBAN_ARCHIVE_HASH_BIN", toolName: "hash-fail", wantCode: "archive_manifest_failed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeArchiveFixture(t, source, "preserve me")
			proposed := filepath.Join(root, "archives", "title")
			tool := filepath.Join(root, tt.toolName)
			if err := os.WriteFile(tool, []byte("#!/usr/bin/env bash\nexit 23\n"), 0o755); err != nil {
				t.Fatal(err)
			}

			result, err := runSeednoteArchiveScript(t, source, proposed, []string{tt.envName + "=" + tool})
			if err == nil {
				t.Fatalf("archive unexpectedly succeeded: %+v", result)
			}
			if result.Status != "recoverable_failure" || result.Code != tt.wantCode || result.Message == "" {
				t.Fatalf("result = %+v, want recoverable %s", result, tt.wantCode)
			}
			assertArchiveFixture(t, source, "preserve me")
			if _, err := os.Stat(proposed); !os.IsNotExist(err) {
				t.Fatalf("failed archive became visible: %v", err)
			}
			assertNoArchiveTemps(t, root)
		})
	}
}

func TestSeednoteArchiveScriptRejectsUnsafeSourceEntries(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, root, source string)
	}{
		{
			name: "file symlink",
			setup: func(t *testing.T, root, source string) {
				t.Helper()
				external := filepath.Join(root, "external-content.md")
				mustWriteArchiveFile(t, external, "external")
				if err := os.Remove(filepath.Join(source, "content.md")); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(external, filepath.Join(source, "content.md")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "directory symlink",
			setup: func(t *testing.T, root, source string) {
				t.Helper()
				external := filepath.Join(root, "external-dir")
				mustWriteArchiveFile(t, filepath.Join(external, "secret.txt"), "external")
				if err := os.Symlink(external, filepath.Join(source, "linked-dir")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "fifo",
			setup: func(t *testing.T, _, source string) {
				t.Helper()
				mkfifo, err := exec.LookPath("mkfifo")
				if err != nil {
					t.Skip("mkfifo is unavailable")
				}
				if output, err := exec.Command(mkfifo, filepath.Join(source, "pipe")).CombinedOutput(); err != nil {
					t.Fatalf("mkfifo: %v\n%s", err, output)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source")
			writeArchiveFixture(t, source, "preserve me")
			tt.setup(t, root, source)
			proposed := filepath.Join(root, "archives", "title")

			result, err := runSeednoteArchiveScript(t, source, proposed, nil)
			if err == nil {
				t.Fatalf("archive unexpectedly succeeded: %+v", result)
			}
			if result.Status != "recoverable_failure" || result.Code != "archive_unsafe_file_type" {
				t.Fatalf("result = %+v, want archive_unsafe_file_type", result)
			}
			if _, err := os.Lstat(filepath.Join(source, "content.md")); err != nil {
				t.Fatalf("source was not preserved: %v", err)
			}
			if _, err := os.Stat(proposed); !os.IsNotExist(err) {
				t.Fatalf("unsafe archive became visible: %v", err)
			}
			assertNoArchiveTemps(t, root)
		})
	}
}

func TestSeednoteArchiveScriptRejectsArchiveParentSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeArchiveFixture(t, source, "preserve me")
	external := t.TempDir()
	archiveLink := filepath.Join(root, "archives")
	if err := os.Symlink(external, archiveLink); err != nil {
		t.Fatal(err)
	}
	proposed := filepath.Join(archiveLink, "title")

	result, err := runSeednoteArchiveScript(t, source, proposed, nil)
	if err == nil {
		t.Fatalf("archive unexpectedly succeeded outside the workspace: %+v", result)
	}
	if result.Status != "recoverable_failure" || result.Code != "archive_unsafe_destination" {
		t.Fatalf("result = %+v, want archive_unsafe_destination", result)
	}
	assertArchiveFixture(t, source, "preserve me")
	if _, err := os.Stat(filepath.Join(external, "title")); !os.IsNotExist(err) {
		t.Fatalf("archive escaped workspace: %v", err)
	}
	assertNoArchiveTemps(t, root)
}

func TestSeednoteArchiveScriptSkipsStaleCandidateReservation(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeArchiveFixture(t, source, "resumed")
	proposed := filepath.Join(root, "archives", "title")
	staleReservation := createArchiveReservation(t, proposed, "stale-owner")

	result, err := runSeednoteArchiveScript(t, source, proposed, nil)
	if err != nil {
		t.Fatalf("archive past stale reservation: %v (%+v)", err, result)
	}
	want := canonicalArchivePath(t, filepath.Join(root, "archives", "title-2"))
	if result.Status != "archived" || result.ArchiveDir != want {
		t.Fatalf("result = %+v, want archive %s", result, want)
	}
	assertArchiveFixture(t, want, "resumed")
	if _, err := os.Stat(staleReservation); err != nil {
		t.Fatalf("stale reservation should be skipped, not deleted: %v", err)
	}
	if err := os.RemoveAll(staleReservation); err != nil {
		t.Fatal(err)
	}
	assertNoArchiveTemps(t, root)
}

func TestSeednoteArchiveScriptLongPauseDoesNotLoseCandidateReservation(t *testing.T) {
	root := t.TempDir()
	sources := []string{filepath.Join(root, "source-a"), filepath.Join(root, "source-b")}
	writeArchiveFixture(t, sources[0], "alpha")
	writeArchiveFixture(t, sources[1], "beta")
	proposed := filepath.Join(root, "archives", "title")
	ready := filepath.Join(root, "mv-ready")
	release := filepath.Join(root, "mv-release")
	shim := filepath.Join(root, "paused-mv")
	mustWriteArchiveFile(t, shim, "#!/usr/bin/env bash\nset -eu\n: > \"$ANBAN_ARCHIVE_MV_READY\"\nwhile [[ ! -e \"$ANBAN_ARCHIVE_MV_RELEASE\" ]]; do sleep 0.02; done\nexec /bin/mv \"$@\"\n")
	if err := os.Chmod(shim, 0o755); err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		result archiveScriptResult
		err    error
	}
	firstDone := make(chan outcome, 1)
	script := seednoteArchiveScriptPath(t)
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o644) })
	go func() {
		result, commandErr, parseErr := executeSeednoteArchiveScript(script, sources[0], proposed, []string{
			"ANBAN_ARCHIVE_MV_BIN=" + shim,
			"ANBAN_ARCHIVE_MV_READY=" + ready,
			"ANBAN_ARCHIVE_MV_RELEASE=" + release,
			"ANBAN_ARCHIVE_LOCK_TTL_SECONDS=1",
		})
		if parseErr != nil {
			firstDone <- outcome{err: parseErr}
			return
		}
		firstDone <- outcome{result: result, err: commandErr}
	}()
	waitForArchivePath(t, ready, 20*time.Second)
	time.Sleep(3 * time.Second)

	second, secondErr := runSeednoteArchiveScript(t, sources[1], proposed, nil)
	if err := os.WriteFile(release, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	first := <-firstDone
	if first.err != nil || secondErr != nil {
		t.Fatalf("paused archive errors: first=%v (%+v), second=%v (%+v)", first.err, first.result, secondErr, second)
	}
	paths := []string{first.result.ArchiveDir, second.ArchiveDir}
	sort.Strings(paths)
	wantPaths := []string{
		canonicalArchivePath(t, proposed),
		canonicalArchivePath(t, filepath.Join(root, "archives", "title-2")),
	}
	if strings.Join(paths, "\n") != strings.Join(wantPaths, "\n") {
		t.Fatalf("archive paths = %v, want %v", paths, wantPaths)
	}
	contents := []string{mustReadArchiveFile(t, filepath.Join(paths[0], "content.md")), mustReadArchiveFile(t, filepath.Join(paths[1], "content.md"))}
	sort.Strings(contents)
	if strings.Join(contents, ",") != "alpha,beta" {
		t.Fatalf("archive contents = %v", contents)
	}
	assertNoArchiveTemps(t, root)
}

func TestSeednoteArchiveScriptDetectsExternalCandidateRace(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeArchiveFixture(t, source, "preserve me")
	proposed := filepath.Join(root, "archives", "title")
	shim := filepath.Join(root, "racing-mv")
	mustWriteArchiveFile(t, shim, "#!/usr/bin/env bash\nset -eu\ncandidate=${!#}\nmkdir -p -- \"$candidate\"\nprintf external > \"$candidate/sentinel\"\nexec /bin/mv \"$@\"\n")
	if err := os.Chmod(shim, 0o755); err != nil {
		t.Fatal(err)
	}

	result, err := runSeednoteArchiveScript(t, source, proposed, []string{"ANBAN_ARCHIVE_MV_BIN=" + shim})
	if err == nil {
		t.Fatalf("external candidate race was reported as success: %+v", result)
	}
	if result.Status != "recoverable_failure" || result.Code != "archive_destination_race" {
		t.Fatalf("result = %+v, want archive_destination_race", result)
	}
	if got := mustReadArchiveFile(t, filepath.Join(proposed, "sentinel")); got != "external" {
		t.Fatalf("external candidate changed: %q", got)
	}
	assertArchiveFixture(t, source, "preserve me")
	if _, err := os.Stat(filepath.Join(proposed, "content.md")); !os.IsNotExist(err) {
		t.Fatalf("staging tree leaked into external candidate: %v", err)
	}
	assertNoArchiveTemps(t, root)
}

func TestSeednoteArchiveScriptRejectsControlCharacterPathWithValidJSON(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "source")
	writeArchiveFixture(t, source, "preserve me")
	mustWriteArchiveFile(t, filepath.Join(source, "unsafe\n\t\x1b.txt"), "unsafe")
	proposed := filepath.Join(root, "archives", "title")

	result, err := runSeednoteArchiveScript(t, source, proposed, nil)
	if err == nil {
		t.Fatalf("archive unexpectedly accepted a control-character path: %+v", result)
	}
	if result.Status != "recoverable_failure" || result.Code != "archive_unsafe_path" {
		t.Fatalf("result = %+v, want archive_unsafe_path", result)
	}
}

func seednoteArchiveScriptPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "claudecode", "scripts", "archive-seednote-workspace.sh")
}

func runSeednoteArchiveScript(t *testing.T, source, proposed string, env []string) (archiveScriptResult, error) {
	t.Helper()
	result, commandErr, parseErr := executeSeednoteArchiveScript(seednoteArchiveScriptPath(t), source, proposed, env)
	if parseErr != nil {
		t.Fatalf("decode archive output: %v (command error: %v)", parseErr, commandErr)
	}
	return result, commandErr
}

func executeSeednoteArchiveScript(script, source, proposed string, env []string) (archiveScriptResult, error, error) {
	cmd := exec.Command(script, source, proposed)
	cmd.Env = append(os.Environ(), env...)
	output, err := cmd.Output()
	if exitErr, ok := err.(*exec.ExitError); ok && len(output) == 0 {
		output = exitErr.Stderr
	}
	var result archiveScriptResult
	if jsonErr := json.Unmarshal([]byte(strings.TrimSpace(string(output))), &result); jsonErr != nil {
		return result, err, fmt.Errorf("decode archive output %q: %w", output, jsonErr)
	}
	return result, err, nil
}

func createArchiveReservation(t *testing.T, candidate, token string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(candidate), 0o755); err != nil {
		t.Fatal(err)
	}
	canonical := canonicalArchivePath(t, candidate)
	hash := sha256.Sum256([]byte(canonical))
	reservation := filepath.Join(filepath.Dir(canonical), fmt.Sprintf(".archive-reserve-%x", hash[:]))
	if err := os.MkdirAll(filepath.Dir(reservation), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(reservation, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteArchiveFile(t, filepath.Join(reservation, "owner"), token+"\n")
	return reservation
}

func waitForArchivePath(t *testing.T, path string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", path)
}

func writeArchiveFixture(t *testing.T, root, content string) {
	t.Helper()
	mustWriteArchiveFile(t, filepath.Join(root, "content.md"), content)
	mustWriteArchiveFile(t, filepath.Join(root, ".trace.json"), "hidden")
	mustWriteArchiveFile(t, filepath.Join(root, "nested", "image.txt"), "nested")
	if err := os.MkdirAll(filepath.Join(root, "empty-dir"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func assertArchiveFixture(t *testing.T, root, content string) {
	t.Helper()
	if got := mustReadArchiveFile(t, filepath.Join(root, "content.md")); got != content {
		t.Fatalf("content.md = %q, want %q", got, content)
	}
	if got := mustReadArchiveFile(t, filepath.Join(root, ".trace.json")); got != "hidden" {
		t.Fatalf(".trace.json = %q", got)
	}
	if got := mustReadArchiveFile(t, filepath.Join(root, "nested", "image.txt")); got != "nested" {
		t.Fatalf("nested/image.txt = %q", got)
	}
	if info, err := os.Stat(filepath.Join(root, "empty-dir")); err != nil || !info.IsDir() {
		t.Fatalf("empty-dir missing or not a directory: %v", err)
	}
}

func mustWriteArchiveFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustReadArchiveFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func assertNoArchiveTemps(t *testing.T, root string) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := entry.Name()
		if strings.HasPrefix(name, ".seednote-") || strings.HasPrefix(name, ".archive-lock-") || strings.HasPrefix(name, ".archive-reserve-") {
			t.Errorf("archive temporary path leaked: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func canonicalArchivePath(t *testing.T, path string) string {
	t.Helper()
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(parent, filepath.Base(path))
}
