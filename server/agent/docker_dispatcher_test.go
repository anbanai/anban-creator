package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	containertypes "github.com/docker/docker/api/types/container"
	imageTypes "github.com/docker/docker/api/types/image"
	networktypes "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/go-connections/nat"
	dockerspec "github.com/moby/docker-image-spec/specs-go/v1"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"

	"github.com/anbanai/anban-creator/server/auth"
	srvconfig "github.com/anbanai/anban-creator/server/config"
	"github.com/anbanai/anban-creator/server/model"
)

const dockerDispatcherTestImageID = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

func TestDockerDispatcherPreparesThenActivatesContainer(t *testing.T) {
	engine := newFakeDockerEngine()
	tokens := dockerDispatcherTestTokens(t)
	dispatcher := newDockerDispatcherForTest(t, engine, tokens)
	execution := dockerDispatcherTestExecution()

	identity, err := dispatcher.Prepare(context.Background(), execution, dockerDispatcherTestTask())
	if err != nil {
		t.Fatal(err)
	}
	if identity.Scope != "docker" || identity.Workload != dockerRuntimeContainerName("docker-execution-1") || identity.InstanceID != engine.containerID {
		t.Fatalf("identity = %#v", identity)
	}
	assertCallSubsequence(t, engine.calls,
		"volume-create:project", "volume-create:task", "container-create", "archive-copy",
	)
	if slices.Contains(engine.calls, "container-start") {
		t.Fatalf("container started during preparation: calls=%v", engine.calls)
	}
	if engine.createdConfig == nil || engine.createdConfig.Image != dockerDispatcherTestImageID {
		t.Fatalf("created config = %#v", engine.createdConfig)
	}
	if !slices.Equal(engine.createdConfig.Env, []string{"PATH=/trusted/bin:/usr/bin", "HOME=/home/node"}) ||
		!maps.Equal(engine.createdConfig.Labels, map[string]string{
			"org.opencontainers.image.authors": "trusted author",
			dockerExecutionIDLabel:             "docker-execution-1",
			dockerTaskIDLabel:                  "docker-task-1",
			dockerProjectIDLabel:               "docker-project-1",
			dockerUserIDLabel:                  "docker-user-1",
		}) {
		t.Fatalf("trusted image config not inherited: %#v", engine.createdConfig)
	}
	if engine.createdConfig.ArgsEscaped {
		t.Fatal("deprecated OCI ArgsEscaped was inherited")
	}
	if engine.createdConfig.Healthcheck == nil || engine.createdConfig.Healthcheck.StartInterval != 3*time.Second ||
		!slices.Equal(engine.createdConfig.Healthcheck.Test, []string{"CMD", "healthcheck"}) {
		t.Fatalf("healthcheck = %#v", engine.createdConfig.Healthcheck)
	}
	if len(engine.copyDestinations) != 1 || engine.copyDestinations[0] != "/run/secrets/anban" {
		t.Fatalf("archive destinations = %v", engine.copyDestinations)
	}
	if len(engine.copyOptions) != 1 || !engine.copyOptions[0].CopyUIDGID {
		t.Fatalf("archive copy options = %#v, want UID/GID preservation", engine.copyOptions)
	}
	header, token := readSingleTarEntry(t, engine.archives[0])
	if header.Name != "token" || header.Mode != 0o400 || header.Uid != 1000 || header.Gid != 1000 || header.Typeflag != tar.TypeReg {
		t.Fatalf("token archive header = %#v", header)
	}
	claims, err := tokens.Validate(token)
	if err != nil {
		t.Fatalf("validate copied token: %v", err)
	}
	if claims.RuntimeScope != identity.Scope || claims.RuntimeWorkload != identity.Workload || claims.RuntimeInstanceID != identity.InstanceID ||
		claims.UserID != "docker-user-1" || claims.ProjectID != "docker-project-1" || claims.TaskID != "docker-task-1" || claims.ExecutionID != "docker-execution-1" {
		t.Fatalf("workload claims = %#v", claims)
	}
	if claims.ExpiresAt == nil || claims.IssuedAt == nil || claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) > time.Hour {
		t.Fatalf("workload token lifetime = %#v", claims.RegisteredClaims)
	}
	if !engine.allDispatchCallsHadDeadline {
		t.Fatal("Docker dispatch did not apply one execution timeout to Engine calls")
	}
	bindDockerRuntimeIdentity(execution, identity)
	if err := dispatcher.Activate(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	assertCallSubsequence(t, engine.calls, "archive-copy", "container-start")
}

func TestDockerDispatcherFencesTagRaceBeforeCopyingWorkloadToken(t *testing.T) {
	const (
		mutableTag   = "registry.example.com/creator-agent-article:latest"
		racedImageID = "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	)
	engine := newFakeDockerEngine()
	engine.images[mutableTag] = dockerDispatcherTestImage()
	engine.createdImageIDs = map[string]string{
		mutableTag:                  racedImageID,
		dockerDispatcherTestImageID: dockerDispatcherTestImageID,
	}
	runtimeImages := dockerDispatcherTestImages()
	runtimeImages[model.PlatformArticle] = mutableTag
	dispatcher, err := NewDockerDispatcher(
		runtimeImages, dockerDispatcherTestConfig(), "http://server:8080",
		dockerDispatcherTestTokens(t), engine, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	execution := dockerDispatcherTestExecution()
	execution.RuntimeImage = mutableTag

	if _, err := dispatcher.Prepare(context.Background(), execution, dockerDispatcherTestTask()); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if engine.createdConfig == nil || engine.createdConfig.Image != dockerDispatcherTestImageID {
		t.Fatalf("ContainerCreate image = %#v, want inspected immutable ID %q", engine.createdConfig, dockerDispatcherTestImageID)
	}
	if !slices.Equal(engine.copiedImageIDs, []string{dockerDispatcherTestImageID}) {
		t.Fatalf("workload token copied to image IDs %v, want only inspected immutable ID %q; raced image %q must receive no token", engine.copiedImageIDs, dockerDispatcherTestImageID, racedImageID)
	}
}

func TestDockerDispatcherResolvePreparedIsLookupOnlyAndInstanceFenced(t *testing.T) {
	ctx := context.Background()
	engine := newFakeDockerEngine()
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))
	execution := dockerDispatcherTestExecution()
	task := dockerDispatcherTestTask()
	prepared, err := dispatcher.Prepare(ctx, execution, task)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	engine.calls = nil

	resolved, err := dispatcher.ResolvePrepared(ctx, execution, task)
	if err != nil {
		t.Fatalf("ResolvePrepared: %v", err)
	}
	if *resolved != *prepared {
		t.Fatalf("resolved identity = %#v, want %#v", resolved, prepared)
	}
	if !slices.Equal(engine.calls, []string{"image-inspect", "container-inspect"}) {
		t.Fatalf("recovery calls = %v, want lookup only", engine.calls)
	}

	replacement := *execution
	replacement.RuntimeInstanceID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := dispatcher.ResolvePrepared(ctx, &replacement, task); err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "instance identity mismatch") {
		t.Fatalf("replacement ResolvePrepared error = %v, want permanent instance mismatch", err)
	}
	if len(engine.removedContainers) != 0 || slices.Contains(engine.calls, "container-start") || slices.Contains(engine.calls, "container-create") {
		t.Fatalf("replacement recovery mutated runtime: calls=%v removed=%v", engine.calls, engine.removedContainers)
	}
}

func TestDockerDispatcherResolvePreparedReportsNotFoundWithoutCreating(t *testing.T) {
	engine := newFakeDockerEngine()
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))
	_, err := dispatcher.ResolvePrepared(context.Background(), dockerDispatcherTestExecution(), dockerDispatcherTestTask())
	if !errors.Is(err, ErrRuntimeWorkloadNotFound) {
		t.Fatalf("ResolvePrepared error = %v, want ErrRuntimeWorkloadNotFound", err)
	}
	if !slices.Equal(engine.calls, []string{"image-inspect", "container-inspect"}) {
		t.Fatalf("missing recovery calls = %v, want lookup only", engine.calls)
	}
}

func TestDockerDispatcherDoesNotStartBeforeRuntimeIdentityPersistence(t *testing.T) {
	engine := newFakeDockerEngine()
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))

	identity, err := dispatcher.Prepare(context.Background(), dockerDispatcherTestExecution(), dockerDispatcherTestTask())
	if err != nil {
		t.Fatal(err)
	}
	if identity == nil || identity.InstanceID == "" {
		t.Fatalf("prepared identity = %#v", identity)
	}
	if slices.Contains(engine.calls, "container-start") {
		t.Fatalf("container started before identity persistence: calls=%v", engine.calls)
	}
}

func TestDockerResumeRequiresExistingTaskVolume(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		configure func(*model.TaskExecution)
	}{
		{name: "resume", configure: func(execution *model.TaskExecution) { execution.ParentExecutionID = "parent-execution" }},
		{name: "retry", configure: func(execution *model.TaskExecution) { execution.ParentExecutionID = "" }},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newFakeDockerEngine()
			execution := dockerDispatcherTestExecution()
			execution.Attempt = 2
			testCase.configure(execution)
			execution.RuntimeProfile = model.PlatformMontage
			execution.RuntimeImage = "registry.example.com/creator-agent-montage@sha256:parent"

			_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), execution, dockerDispatcherTestTask())
			if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "original task workspace") {
				t.Fatalf("Dispatch error = %v, want permanent original task workspace error", err)
			}
			if slices.Contains(engine.calls, "volume-create:task") || slices.Contains(engine.calls, "container-create") {
				t.Fatalf("resumed execution created missing workspace or container: calls=%v", engine.calls)
			}
			if !slices.Contains(engine.calls, "volume-create:project") {
				t.Fatalf("resumed execution did not create-or-verify project memory: calls=%v", engine.calls)
			}
		})
	}
}

func TestDockerDispatcherConvertsTrustedOCIImageConfig(t *testing.T) {
	trusted := dockerDispatcherTestImage().Config
	converted := dockerContainerConfigFromInspect(trusted)
	if !slices.Equal(converted.Env, trusted.Env) ||
		!maps.Equal(converted.ExposedPorts, map[nat.Port]struct{}{"9090/tcp": {}}) ||
		!maps.Equal(converted.Volumes, trusted.Volumes) ||
		!maps.Equal(converted.Labels, trusted.Labels) ||
		converted.StopSignal != trusted.StopSignal ||
		!slices.Equal(converted.Shell, trusted.Shell) {
		t.Fatalf("converted config = %#v, trusted = %#v", converted, trusted)
	}
	if converted.Healthcheck == nil || converted.Healthcheck.Interval != trusted.Healthcheck.Interval ||
		converted.Healthcheck.Timeout != trusted.Healthcheck.Timeout || converted.Healthcheck.StartPeriod != trusted.Healthcheck.StartPeriod ||
		converted.Healthcheck.StartInterval != trusted.Healthcheck.StartInterval || converted.Healthcheck.Retries != trusted.Healthcheck.Retries ||
		!slices.Equal(converted.Healthcheck.Test, trusted.Healthcheck.Test) {
		t.Fatalf("converted healthcheck = %#v, trusted = %#v", converted.Healthcheck, trusted.Healthcheck)
	}
	if converted.ArgsEscaped {
		t.Fatal("OCI ArgsEscaped was trusted")
	}
	if got := dockerContainerConfigFromInspect(&dockerspec.DockerOCIImageConfig{}); got.Env != nil || got.ExposedPorts != nil || got.Volumes != nil || got.Labels != nil || got.Healthcheck != nil || got.Shell != nil {
		t.Fatalf("nil image defaults were not preserved: %#v", got)
	}
	empty := dockerContainerConfigFromInspect(&dockerspec.DockerOCIImageConfig{ImageConfig: ocispec.ImageConfig{
		Env: []string{}, ExposedPorts: map[string]struct{}{}, Volumes: map[string]struct{}{}, Labels: map[string]string{},
	}, DockerOCIImageConfigExt: dockerspec.DockerOCIImageConfigExt{Shell: []string{}, Healthcheck: &dockerspec.HealthcheckConfig{Test: []string{}}}})
	if empty.Env == nil || empty.ExposedPorts == nil || empty.Volumes == nil || empty.Labels == nil || empty.Shell == nil || empty.Healthcheck == nil || empty.Healthcheck.Test == nil {
		t.Fatalf("explicit empty image defaults were collapsed to nil: %#v", empty)
	}
}

func TestDockerDispatcherRecoversCreateRacesAndRejectsDrift(t *testing.T) {
	t.Run("matching create races", func(t *testing.T) {
		engine := newFakeDockerEngine()
		engine.conflictProjectCreate = true
		engine.conflictTaskCreate = true
		engine.conflictContainerCreate = true
		identity, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), dockerDispatcherTestExecution(), dockerDispatcherTestTask())
		if err != nil {
			t.Fatal(err)
		}
		if identity.InstanceID != engine.containerID {
			t.Fatalf("identity = %#v", identity)
		}
		assertCallSubsequence(t, engine.calls, "volume-create:project", "volume-create:task", "container-create", "archive-copy")
	})

	t.Run("foreign volume", func(t *testing.T) {
		engine := newFakeDockerEngine()
		task := dockerDispatcherTestTask()
		name := dockerProjectMemoryVolumeName(task.ProjectID)
		engine.volumes[name] = volume.Volume{Name: name, Driver: dockerVolumeDriver, Labels: map[string]string{
			dockerProjectIDLabel: task.ProjectID,
			dockerUserIDLabel:    "foreign-user",
		}}
		_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), dockerDispatcherTestExecution(), task)
		if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "drift") {
			t.Fatalf("Dispatch error = %v, want permanent volume drift", err)
		}
		if slices.Contains(engine.calls, "container-create") {
			t.Fatalf("container created after volume drift: %v", engine.calls)
		}
	})

	t.Run("foreign container wins create race", func(t *testing.T) {
		engine := newFakeDockerEngine()
		engine.conflictContainerCreate = true
		engine.foreignContainerOnConflict = true
		_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), dockerDispatcherTestExecution(), dockerDispatcherTestTask())
		if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "drift") {
			t.Fatalf("Dispatch error = %v, want permanent container drift", err)
		}
		if slices.Contains(engine.calls, "archive-copy") || slices.Contains(engine.calls, "container-start") {
			t.Fatalf("foreign container was taken over: %v", engine.calls)
		}
	})
}

func TestDockerDispatcherRejectsReplacedPersistedContainer(t *testing.T) {
	engine := newFakeDockerEngine()
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))
	execution := dockerDispatcherTestExecution()
	identity, err := dispatcher.Prepare(context.Background(), execution, dockerDispatcherTestTask())
	if err != nil {
		t.Fatal(err)
	}
	execution.RuntimeScope = identity.Scope
	execution.RuntimeWorkload = identity.Workload
	execution.RuntimeInstanceID = "persisted-container-instance"
	engine.calls = nil

	_, err = dispatcher.Prepare(context.Background(), execution, dockerDispatcherTestTask())
	if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "instance identity mismatch") {
		t.Fatalf("Dispatch error = %v, want permanent persisted instance mismatch", err)
	}
	if slices.Contains(engine.calls, "archive-copy") || slices.Contains(engine.calls, "container-start") {
		t.Fatalf("replacement container was taken over: calls=%v", engine.calls)
	}
}

func TestDockerDispatcherRejectsMissingPersistedContainerBeforeMutations(t *testing.T) {
	engine := newFakeDockerEngine()
	execution := dockerDispatcherTestExecution()
	execution.RuntimeScope = dockerRuntimeScope
	execution.RuntimeWorkload = dockerRuntimeContainerName(execution.ID)
	execution.RuntimeInstanceID = "persisted-container-instance"

	_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), execution, dockerDispatcherTestTask())
	if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "instance identity mismatch") {
		t.Fatalf("Dispatch error = %v, want permanent missing persisted instance", err)
	}
	for _, mutation := range []string{"volume-create:project", "volume-create:task", "container-create", "archive-copy", "container-start"} {
		if slices.Contains(engine.calls, mutation) {
			t.Fatalf("Docker mutation %q occurred before persisted instance validation: calls=%v", mutation, engine.calls)
		}
	}
}

func TestDockerDispatcherClassifiesPermanentInspectErrors(t *testing.T) {
	t.Run("volume inspect", func(t *testing.T) {
		engine := newFakeDockerEngine()
		engine.volumeInspectError = errdefs.Forbidden(errors.New("volume access denied"))
		_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), dockerDispatcherTestExecution(), dockerDispatcherTestTask())
		if err == nil || !IsPermanentDispatchError(err) {
			t.Fatalf("Dispatch error = %v, want permanent volume inspect error", err)
		}
	})

	t.Run("container inspect", func(t *testing.T) {
		engine := newFakeDockerEngine()
		engine.containerInspectError = errdefs.Unauthorized(errors.New("container access denied"))
		_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), dockerDispatcherTestExecution(), dockerDispatcherTestTask())
		if err == nil || !IsPermanentDispatchError(err) {
			t.Fatalf("Dispatch error = %v, want permanent container inspect error", err)
		}
	})
}

func TestDockerDispatcherTreatsAlreadyStartedContainerAsDesiredState(t *testing.T) {
	engine := newFakeDockerEngine()
	engine.startError = errdefs.NotModified(errors.New("container already started"))
	execution := dockerDispatcherTestExecution()
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))

	identity, err := dispatcher.Prepare(context.Background(), execution, dockerDispatcherTestTask())
	if err != nil {
		t.Fatalf("prepare container: %v", err)
	}
	if identity == nil || identity.Scope != dockerRuntimeScope || identity.Workload != dockerRuntimeContainerName(dockerDispatcherTestExecution().ID) || identity.InstanceID != engine.containerID {
		t.Fatalf("identity = %#v", identity)
	}
	bindDockerRuntimeIdentity(execution, identity)
	if err := dispatcher.Activate(context.Background(), execution); err != nil {
		t.Fatalf("activate already-started container: %v", err)
	}
	assertCallSubsequence(t, engine.calls, "container-create", "archive-copy", "container-start")
}

func TestDockerDispatcherContainerDisappearanceBeforePersistenceIsRetryable(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeDockerEngine)
	}{
		{name: "copy token", configure: func(engine *fakeDockerEngine) {
			engine.copyError = errdefs.NotFound(errors.New("container disappeared before archive copy"))
		}},
		{name: "create conflict inspect", configure: func(engine *fakeDockerEngine) {
			engine.conflictContainerCreate = true
			engine.containerMissingOnConflict = true
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newFakeDockerEngine()
			testCase.configure(engine)
			_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), dockerDispatcherTestExecution(), dockerDispatcherTestTask())
			if err == nil {
				t.Fatal("Dispatch succeeded after pre-persistence container disappearance")
			}
			if IsPermanentDispatchError(err) {
				t.Fatalf("Dispatch error = %v, want retryable container disappearance", err)
			}
		})
	}
}

func TestDockerActivationIsIdentityBoundAndIdempotent(t *testing.T) {
	engine := newFakeDockerEngine()
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))
	execution := dockerDispatcherTestExecution()
	identity, err := dispatcher.Prepare(context.Background(), execution, dockerDispatcherTestTask())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("mismatched persisted instance", func(t *testing.T) {
		mismatched := *execution
		bindDockerRuntimeIdentity(&mismatched, identity)
		mismatched.RuntimeInstanceID = "replacement-instance"
		err := dispatcher.Activate(context.Background(), &mismatched)
		if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "instance identity mismatch") {
			t.Fatalf("Activate error = %v, want permanent instance mismatch", err)
		}
	})

	bindDockerRuntimeIdentity(execution, identity)
	if err := dispatcher.Activate(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	engine.calls = nil
	if err := dispatcher.Activate(context.Background(), execution); err != nil {
		t.Fatalf("idempotent Activate: %v", err)
	}
	if slices.Contains(engine.calls, "container-start") {
		t.Fatalf("running container was started again: calls=%v", engine.calls)
	}
}

func TestDockerActivationDoesNotRecreateMissingContainer(t *testing.T) {
	engine := newFakeDockerEngine()
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))
	execution := dockerDispatcherTestExecution()
	bindDockerRuntimeIdentity(execution, &model.RuntimeIdentity{
		Scope: dockerRuntimeScope, Workload: dockerRuntimeContainerName(execution.ID), InstanceID: "missing-instance",
	})
	err := dispatcher.Activate(context.Background(), execution)
	if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("Activate error = %v, want permanent missing container", err)
	}
	for _, call := range engine.calls {
		if strings.Contains(call, "create") {
			t.Fatalf("activation recreated missing resource: calls=%v", engine.calls)
		}
	}
}

func TestDockerDispatcherValidatesBeforeDockerMutation(t *testing.T) {
	tests := []struct {
		name      string
		execution *model.TaskExecution
		task      *model.Task
		want      string
	}{
		{name: "missing execution", task: dockerDispatcherTestTask(), want: "execution is required"},
		{name: "incomplete task", execution: dockerDispatcherTestExecution(), task: &model.Task{ID: "docker-task-1"}, want: "task identity is required"},
		{name: "task mismatch", execution: dockerDispatcherTestExecution(), task: &model.Task{ID: "other", UserID: "u", ProjectID: "p", Type: model.PlatformArticle}, want: "execution task identity mismatch"},
		{name: "missing runtime", execution: func() *model.TaskExecution { e := dockerDispatcherTestExecution(); e.RuntimeImage = ""; return e }(), task: dockerDispatcherTestTask(), want: "runtime identity is required"},
		{name: "initial runtime drift", execution: func() *model.TaskExecution { e := dockerDispatcherTestExecution(); e.RuntimeImage = "other"; return e }(), task: dockerDispatcherTestTask(), want: "initial runtime identity mismatch"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newFakeDockerEngine()
			_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Prepare(context.Background(), testCase.execution, testCase.task)
			if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("Dispatch error = %v, want permanent %q", err, testCase.want)
			}
			if len(engine.calls) != 0 {
				t.Fatalf("Docker called before identity validation: %v", engine.calls)
			}
		})
	}
}

func TestDockerInspectMapsContainerStates(t *testing.T) {
	finished := time.Now().UTC().Truncate(time.Second)
	tests := []struct {
		name          string
		status        containertypes.ContainerState
		exitCode      int
		dead          bool
		oom           bool
		message       string
		finishedAt    string
		wantPhase     string
		wantReason    string
		wantExitCode  *int32
		wantCompleted bool
	}{
		{name: "created", status: containertypes.StateCreated, wantPhase: RuntimePhasePending, wantReason: "Created"},
		{name: "running", status: containertypes.StateRunning, wantPhase: RuntimePhaseRunning, wantReason: "Running"},
		{name: "exited zero", status: containertypes.StateExited, finishedAt: finished.Format(time.RFC3339Nano), wantPhase: RuntimePhaseSucceeded, wantReason: "Exited", wantExitCode: dockerInt32(0), wantCompleted: true},
		{name: "exited nonzero", status: containertypes.StateExited, exitCode: 2, message: "command failed", finishedAt: finished.Format(time.RFC3339Nano), wantPhase: RuntimePhaseFailed, wantReason: "Exited", wantExitCode: dockerInt32(2), wantCompleted: true},
		{name: "oom killed", status: containertypes.StateExited, exitCode: 137, oom: true, wantPhase: RuntimePhaseFailed, wantReason: "OOMKilled", wantExitCode: dockerInt32(137)},
		{name: "dead", status: containertypes.StateDead, exitCode: 1, dead: true, message: "daemon failure", wantPhase: RuntimePhaseFailed, wantReason: "Dead", wantExitCode: dockerInt32(1)},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			engine := newFakeDockerEngine()
			execution := dockerDispatcherTestExecution()
			engine.containers[dockerRuntimeContainerName(execution.ID)] = dockerIdentityInspect(execution, engine.containerID, &containertypes.State{
				Status: testCase.status, Running: testCase.status == containertypes.StateRunning, Dead: testCase.dead, OOMKilled: testCase.oom,
				ExitCode: testCase.exitCode, Error: testCase.message, FinishedAt: testCase.finishedAt,
			})
			state, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Inspect(context.Background(), execution)
			if err != nil {
				t.Fatal(err)
			}
			if state.Phase != testCase.wantPhase || state.InstanceID != engine.containerID || state.Reason != testCase.wantReason || state.Message != testCase.message || !reflect.DeepEqual(state.ExitCode, testCase.wantExitCode) {
				t.Fatalf("state = %#v", state)
			}
			if testCase.wantCompleted != (state.CompletedAt != nil) || state.CompletedAt != nil && !state.CompletedAt.Equal(finished) {
				t.Fatalf("CompletedAt = %v", state.CompletedAt)
			}
		})
	}

	engine := newFakeDockerEngine()
	_, err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Inspect(context.Background(), dockerDispatcherTestExecution())
	if !errors.Is(err, ErrRuntimeWorkloadNotFound) {
		t.Fatalf("Inspect missing error = %v, want ErrRuntimeWorkloadNotFound", err)
	}
}

func TestDockerDeleteStopsAndRemovesOnlyOwnedContainer(t *testing.T) {
	engine := newFakeDockerEngine()
	execution := dockerDispatcherTestExecution()
	execution.RuntimeScope = "docker"
	execution.RuntimeWorkload = dockerRuntimeContainerName(execution.ID)
	execution.RuntimeInstanceID = engine.containerID
	engine.containers[execution.RuntimeWorkload] = dockerIdentityInspect(execution, engine.containerID, &containertypes.State{Status: containertypes.StateRunning, Running: true})
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))

	if err := dispatcher.Delete(context.Background(), execution); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(engine.stoppedContainers, []string{engine.containerID}) || !slices.Equal(engine.removedContainers, []string{engine.containerID}) {
		t.Fatalf("stopped=%v removed=%v", engine.stoppedContainers, engine.removedContainers)
	}
	if len(engine.removedVolumes) != 0 || engine.removeContainerOptions[0].RemoveVolumes {
		t.Fatalf("terminal delete removed volumes: volumes=%v options=%#v", engine.removedVolumes, engine.removeContainerOptions)
	}
	if err := dispatcher.Delete(context.Background(), execution); err != nil {
		t.Fatalf("missing container delete is not idempotent: %v", err)
	}

	foreign := newFakeDockerEngine()
	bad := dockerIdentityInspect(execution, foreign.containerID, &containertypes.State{Status: containertypes.StateRunning, Running: true})
	bad.Config.Labels[dockerExecutionIDLabel] = "foreign"
	foreign.containers[execution.RuntimeWorkload] = bad
	err := newDockerDispatcherForTest(t, foreign, dockerDispatcherTestTokens(t)).Delete(context.Background(), execution)
	if err == nil || len(foreign.stoppedContainers) != 0 || len(foreign.removedContainers) != 0 {
		t.Fatalf("foreign container delete error=%v stopped=%v removed=%v", err, foreign.stoppedContainers, foreign.removedContainers)
	}
}

func TestDockerDeleteRejectsEmptyRuntimeIdentity(t *testing.T) {
	engine := newFakeDockerEngine()
	execution := dockerDispatcherTestExecution()
	engine.containers[dockerRuntimeContainerName(execution.ID)] = dockerIdentityInspect(execution, engine.containerID, &containertypes.State{
		Status: containertypes.StateRunning, Running: true,
	})
	err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Delete(context.Background(), execution)
	if err == nil || !IsPermanentDispatchError(err) || !strings.Contains(err.Error(), "identity") {
		t.Fatalf("Delete error = %v, want permanent incomplete identity error", err)
	}
	if len(engine.stoppedContainers) != 0 || len(engine.removedContainers) != 0 {
		t.Fatalf("empty-identity delete mutated container: stopped=%v removed=%v", engine.stoppedContainers, engine.removedContainers)
	}
}

func TestDockerDeleteTreatsAlreadyStoppedContainerAsDesiredState(t *testing.T) {
	engine := newFakeDockerEngine()
	engine.stopError = errdefs.NotModified(errors.New("container already stopped"))
	execution := dockerDispatcherTestExecution()
	execution.RuntimeScope = dockerRuntimeScope
	execution.RuntimeWorkload = dockerRuntimeContainerName(execution.ID)
	execution.RuntimeInstanceID = engine.containerID
	engine.containers[execution.RuntimeWorkload] = dockerIdentityInspect(execution, engine.containerID, &containertypes.State{Status: containertypes.StateRunning, Running: true})

	if err := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t)).Delete(context.Background(), execution); err != nil {
		t.Fatalf("Delete already-stopped container: %v", err)
	}
	if !slices.Equal(engine.removedContainers, []string{engine.containerID}) {
		t.Fatalf("removed containers = %v, want inspected instance", engine.removedContainers)
	}
}

func TestDeleteTaskWorkspaceAndProjectMemoryVerifyOwnership(t *testing.T) {
	engine := newFakeDockerEngine()
	task := dockerDispatcherTestTask()
	spec := buildDockerRuntimeSpec(dockerRuntimeConfig{DockerConfig: dockerDispatcherTestConfig(), ServerURL: "http://server:8080"}, dockerDispatcherTestExecution(), task)
	engine.volumes[spec.TaskVolume.Name] = fakeDockerVolume(spec.TaskVolume)
	engine.volumes[spec.ProjectVolume.Name] = fakeDockerVolume(spec.ProjectVolume)
	dispatcher := newDockerDispatcherForTest(t, engine, dockerDispatcherTestTokens(t))

	if err := dispatcher.DeleteTaskWorkspace(context.Background(), task); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.DeleteProjectMemory(context.Background(), task.ProjectID); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(engine.removedVolumes, []string{spec.TaskVolume.Name, spec.ProjectVolume.Name}) {
		t.Fatalf("removed volumes = %v", engine.removedVolumes)
	}
	if err := dispatcher.DeleteTaskWorkspace(context.Background(), task); err != nil {
		t.Fatalf("missing task volume delete is not idempotent: %v", err)
	}
	if err := dispatcher.DeleteProjectMemory(context.Background(), task.ProjectID); err != nil {
		t.Fatalf("missing project volume delete is not idempotent: %v", err)
	}

	t.Run("task volume drift", func(t *testing.T) {
		foreign := newFakeDockerEngine()
		got := fakeDockerVolume(spec.TaskVolume)
		got.Driver = "foreign"
		foreign.volumes[spec.TaskVolume.Name] = got
		err := newDockerDispatcherForTest(t, foreign, dockerDispatcherTestTokens(t)).DeleteTaskWorkspace(context.Background(), task)
		if err == nil || len(foreign.removedVolumes) != 0 {
			t.Fatalf("DeleteTaskWorkspace error=%v removed=%v", err, foreign.removedVolumes)
		}
	})

	t.Run("project volume foreign labels", func(t *testing.T) {
		foreign := newFakeDockerEngine()
		got := fakeDockerVolume(spec.ProjectVolume)
		got.Labels["foreign"] = "resource"
		foreign.volumes[spec.ProjectVolume.Name] = got
		err := newDockerDispatcherForTest(t, foreign, dockerDispatcherTestTokens(t)).DeleteProjectMemory(context.Background(), task.ProjectID)
		if err == nil || len(foreign.removedVolumes) != 0 {
			t.Fatalf("DeleteProjectMemory error=%v removed=%v", err, foreign.removedVolumes)
		}
	})
}

func newDockerDispatcherForTest(t *testing.T, engine *fakeDockerEngine, tokens *auth.WorkloadTokenService) *DockerDispatcher {
	t.Helper()
	dispatcher, err := NewDockerDispatcher(
		dockerDispatcherTestImages(), dockerDispatcherTestConfig(), "http://server:8080", tokens, engine, time.Now,
	)
	if err != nil {
		t.Fatal(err)
	}
	return dispatcher
}

func dockerDispatcherTestTokens(t *testing.T) *auth.WorkloadTokenService {
	t.Helper()
	tokens, err := auth.NewWorkloadTokenService("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return tokens
}

func dockerDispatcherTestConfig() srvconfig.DockerConfig {
	return srvconfig.DockerConfig{Network: "anban", CPUCores: 2, MemoryMB: 4096, PidsLimit: 256, TimeoutSec: 7200}
}

func dockerDispatcherTestImages() srvconfig.RuntimeImages {
	return srvconfig.RuntimeImages{
		model.PlatformArticle:  "registry.example.com/creator-agent-article@sha256:article",
		model.PlatformSeednote: "registry.example.com/creator-agent-seednote@sha256:seednote",
		model.PlatformMontage:  "registry.example.com/creator-agent-montage@sha256:montage",
	}
}

func dockerDispatcherTestExecution() *model.TaskExecution {
	return &model.TaskExecution{
		ID: "docker-execution-1", TaskID: "docker-task-1", Attempt: 1,
		RuntimeProfile: model.PlatformArticle, RuntimeImage: dockerDispatcherTestImages()[model.PlatformArticle],
	}
}

func bindDockerRuntimeIdentity(execution *model.TaskExecution, identity *model.RuntimeIdentity) {
	execution.RuntimeScope = identity.Scope
	execution.RuntimeWorkload = identity.Workload
	execution.RuntimeInstanceID = identity.InstanceID
}

func dockerDispatcherTestTask() *model.Task {
	return &model.Task{ID: "docker-task-1", UserID: "docker-user-1", ProjectID: "docker-project-1", Type: model.PlatformArticle}
}

func dockerDispatcherTestImage() imageTypes.InspectResponse {
	return imageTypes.InspectResponse{
		ID: dockerDispatcherTestImageID,
		Config: &dockerspec.DockerOCIImageConfig{
			ImageConfig: ocispec.ImageConfig{
				Env:          []string{"PATH=/trusted/bin:/usr/bin", "HOME=/home/node"},
				ExposedPorts: map[string]struct{}{"9090/tcp": {}},
				Volumes:      map[string]struct{}{"/trusted-data": {}},
				Labels:       map[string]string{"org.opencontainers.image.authors": "trusted author"},
				StopSignal:   "SIGTERM",
				ArgsEscaped:  true,
			},
			DockerOCIImageConfigExt: dockerspec.DockerOCIImageConfigExt{
				Healthcheck: &dockerspec.HealthcheckConfig{Test: []string{"CMD", "healthcheck"}, Interval: 10 * time.Second, Timeout: 2 * time.Second, StartPeriod: 5 * time.Second, StartInterval: 3 * time.Second, Retries: 2},
				Shell:       []string{"/bin/sh", "-c"},
			},
		},
	}
}

type fakeDockerEngine struct {
	images                      map[string]imageTypes.InspectResponse
	volumes                     map[string]volume.Volume
	containers                  map[string]containertypes.InspectResponse
	calls                       []string
	containerID                 string
	createdConfig               *containertypes.Config
	createdHostConfig           *containertypes.HostConfig
	createdImageIDs             map[string]string
	copyDestinations            []string
	copyOptions                 []containertypes.CopyToContainerOptions
	copiedImageIDs              []string
	archives                    [][]byte
	stoppedContainers           []string
	removedContainers           []string
	removeContainerOptions      []containertypes.RemoveOptions
	removedVolumes              []string
	conflictProjectCreate       bool
	conflictTaskCreate          bool
	conflictContainerCreate     bool
	foreignContainerOnConflict  bool
	containerMissingOnConflict  bool
	volumeInspectError          error
	containerInspectError       error
	copyError                   error
	startError                  error
	stopError                   error
	allDispatchCallsHadDeadline bool
}

func newFakeDockerEngine() *fakeDockerEngine {
	image := dockerDispatcherTestImage()
	return &fakeDockerEngine{
		images: map[string]imageTypes.InspectResponse{
			dockerDispatcherTestImages()[model.PlatformArticle]:        image,
			"registry.example.com/creator-agent-montage@sha256:parent": image,
		},
		volumes: map[string]volume.Volume{}, containers: map[string]containertypes.InspectResponse{},
		containerID:                 "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		allDispatchCallsHadDeadline: true,
	}
}

func (e *fakeDockerEngine) record(ctx context.Context, call string) {
	e.calls = append(e.calls, call)
	if _, ok := ctx.Deadline(); !ok {
		e.allDispatchCallsHadDeadline = false
	}
}

func (e *fakeDockerEngine) ImageInspect(ctx context.Context, image string, _ ...dockerclient.ImageInspectOption) (imageTypes.InspectResponse, error) {
	e.record(ctx, "image-inspect")
	inspected, ok := e.images[image]
	if !ok {
		return imageTypes.InspectResponse{}, errdefs.NotFound(errors.New("image not found"))
	}
	return inspected, nil
}

func (e *fakeDockerEngine) VolumeInspect(ctx context.Context, name string) (volume.Volume, error) {
	e.record(ctx, "volume-inspect:"+name)
	if e.volumeInspectError != nil {
		return volume.Volume{}, e.volumeInspectError
	}
	got, ok := e.volumes[name]
	if !ok {
		return volume.Volume{}, errdefs.NotFound(errors.New("volume not found"))
	}
	return got, nil
}

func (e *fakeDockerEngine) VolumeCreate(ctx context.Context, desired volume.CreateOptions) (volume.Volume, error) {
	kind := "task"
	conflict := e.conflictTaskCreate
	if _, ok := desired.Labels[dockerTaskIDLabel]; !ok {
		kind = "project"
		conflict = e.conflictProjectCreate
	}
	e.record(ctx, "volume-create:"+kind)
	created := fakeDockerVolume(desired)
	e.volumes[desired.Name] = created
	if conflict {
		return volume.Volume{}, errdefs.Conflict(errors.New("volume already exists"))
	}
	return created, nil
}

func (e *fakeDockerEngine) VolumeRemove(ctx context.Context, name string, _ bool) error {
	e.calls = append(e.calls, "volume-remove:"+name)
	if _, ok := e.volumes[name]; !ok {
		return errdefs.NotFound(errors.New("volume not found"))
	}
	delete(e.volumes, name)
	e.removedVolumes = append(e.removedVolumes, name)
	return nil
}

func (e *fakeDockerEngine) ContainerInspect(ctx context.Context, name string) (containertypes.InspectResponse, error) {
	e.record(ctx, "container-inspect")
	if e.containerInspectError != nil {
		return containertypes.InspectResponse{}, e.containerInspectError
	}
	got, ok := e.containers[name]
	if !ok {
		for _, candidate := range e.containers {
			if candidate.ID == name {
				return candidate, nil
			}
		}
		return containertypes.InspectResponse{}, errdefs.NotFound(errors.New("container not found"))
	}
	return got, nil
}

func (e *fakeDockerEngine) ContainerCreate(ctx context.Context, config *containertypes.Config, hostConfig *containertypes.HostConfig, _ *networktypes.NetworkingConfig, _ *ocispec.Platform, name string) (containertypes.CreateResponse, error) {
	e.record(ctx, "container-create")
	e.createdConfig = config
	e.createdHostConfig = hostConfig
	createdImageID := dockerDispatcherTestImageID
	if resolved, ok := e.createdImageIDs[config.Image]; ok {
		createdImageID = resolved
	}
	inspected := fakeDockerContainerInspect(e.containerID, name, createdImageID, config, hostConfig)
	if e.foreignContainerOnConflict {
		inspected.Config.Labels[dockerExecutionIDLabel] = "foreign"
	}
	if !e.containerMissingOnConflict {
		e.containers[name] = inspected
	}
	if e.conflictContainerCreate {
		return containertypes.CreateResponse{}, errdefs.Conflict(errors.New("container already exists"))
	}
	return containertypes.CreateResponse{ID: e.containerID}, nil
}

func (e *fakeDockerEngine) CopyToContainer(ctx context.Context, containerID string, destination string, content io.Reader, options containertypes.CopyToContainerOptions) error {
	e.record(ctx, "archive-copy")
	if e.copyError != nil {
		return e.copyError
	}
	for _, candidate := range e.containers {
		if candidate.ID == containerID {
			e.copiedImageIDs = append(e.copiedImageIDs, candidate.Image)
			break
		}
	}
	raw, err := io.ReadAll(content)
	if err != nil {
		return err
	}
	e.copyDestinations = append(e.copyDestinations, destination)
	e.copyOptions = append(e.copyOptions, options)
	e.archives = append(e.archives, raw)
	return nil
}

func (e *fakeDockerEngine) ContainerStart(ctx context.Context, name string, _ containertypes.StartOptions) error {
	e.record(ctx, "container-start")
	if e.startError != nil {
		return e.startError
	}
	got, ok := e.containers[name]
	if !ok {
		for key, candidate := range e.containers {
			if candidate.ID == name {
				got = candidate
				name = key
				ok = true
				break
			}
		}
		if !ok {
			return errdefs.NotFound(errors.New("container not found"))
		}
	}
	got.State = &containertypes.State{Status: containertypes.StateRunning, Running: true}
	e.containers[name] = got
	return nil
}

func (e *fakeDockerEngine) ContainerStop(_ context.Context, name string, _ containertypes.StopOptions) error {
	e.calls = append(e.calls, "container-stop")
	e.stoppedContainers = append(e.stoppedContainers, name)
	if e.stopError != nil {
		return e.stopError
	}
	return nil
}

func (e *fakeDockerEngine) ContainerRemove(_ context.Context, name string, options containertypes.RemoveOptions) error {
	e.calls = append(e.calls, "container-remove")
	found := false
	for key, candidate := range e.containers {
		if key == name || candidate.ID == name {
			delete(e.containers, key)
			found = true
		}
	}
	if !found {
		return errdefs.NotFound(errors.New("container not found"))
	}
	e.removedContainers = append(e.removedContainers, name)
	e.removeContainerOptions = append(e.removeContainerOptions, options)
	return nil
}

func fakeDockerVolume(desired volume.CreateOptions) volume.Volume {
	return volume.Volume{Name: desired.Name, Driver: desired.Driver, Labels: maps.Clone(desired.Labels), Options: maps.Clone(desired.DriverOpts)}
}

func fakeDockerContainerInspect(id, name, imageID string, config *containertypes.Config, hostConfig *containertypes.HostConfig) containertypes.InspectResponse {
	inspectedConfig := *config
	inspectedConfig.Hostname = id[:12]
	inspectedConfig.Labels = maps.Clone(config.Labels)
	inspectedConfig.Env = slices.Clone(config.Env)
	inspectedConfig.Cmd = slices.Clone(config.Cmd)
	inspectedConfig.Entrypoint = slices.Clone(config.Entrypoint)
	inspectedHostConfig := *hostConfig
	inspectedHostConfig.MaskedPaths = slices.Clone(dockerRequiredMaskedPaths)
	inspectedHostConfig.ReadonlyPaths = slices.Clone(dockerRequiredReadonlyPaths)
	return containertypes.InspectResponse{
		ContainerJSONBase: &containertypes.ContainerJSONBase{
			ID: id, Name: "/" + name, Image: imageID, State: &containertypes.State{Status: containertypes.StateCreated}, HostConfig: &inspectedHostConfig,
		},
		Config: &inspectedConfig,
		NetworkSettings: &containertypes.NetworkSettings{Networks: map[string]*networktypes.EndpointSettings{
			string(hostConfig.NetworkMode): {
				Aliases:    []string{id[:12]},
				DNSNames:   []string{name, id[:12]},
				NetworkID:  "network-id",
				EndpointID: "endpoint-id",
			},
		}},
	}
}

func dockerIdentityInspect(execution *model.TaskExecution, id string, state *containertypes.State) containertypes.InspectResponse {
	return containertypes.InspectResponse{
		ContainerJSONBase: &containertypes.ContainerJSONBase{ID: id, Name: "/" + dockerRuntimeContainerName(execution.ID), Image: dockerDispatcherTestImageID, State: state},
		Config: &containertypes.Config{Image: dockerDispatcherTestImageID, Labels: map[string]string{
			dockerExecutionIDLabel: execution.ID,
			dockerTaskIDLabel:      execution.TaskID,
			dockerProjectIDLabel:   "docker-project-1",
			dockerUserIDLabel:      "docker-user-1",
		}},
	}
}

func assertCallSubsequence(t *testing.T, calls []string, want ...string) {
	t.Helper()
	position := 0
	for _, call := range calls {
		if position < len(want) && call == want[position] {
			position++
		}
	}
	if position != len(want) {
		t.Fatalf("calls = %v, want ordered subsequence %v", calls, want)
	}
}

func readSingleTarEntry(t *testing.T, raw []byte) (*tar.Header, string) {
	t.Helper()
	reader := tar.NewReader(bytes.NewReader(raw))
	header, err := reader.Next()
	if err != nil {
		t.Fatal(err)
	}
	contents, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if next, err := reader.Next(); err != io.EOF || next != nil {
		t.Fatalf("token archive has extra entry: header=%#v err=%v", next, err)
	}
	return header, string(contents)
}

func dockerInt32(value int32) *int32 { return &value }

var _ interface {
	ImageInspect(context.Context, string, ...dockerclient.ImageInspectOption) (imageTypes.InspectResponse, error)
	VolumeInspect(context.Context, string) (volume.Volume, error)
	VolumeCreate(context.Context, volume.CreateOptions) (volume.Volume, error)
	VolumeRemove(context.Context, string, bool) error
	ContainerInspect(context.Context, string) (containertypes.InspectResponse, error)
	ContainerCreate(context.Context, *containertypes.Config, *containertypes.HostConfig, *networktypes.NetworkingConfig, *ocispec.Platform, string) (containertypes.CreateResponse, error)
	CopyToContainer(context.Context, string, string, io.Reader, containertypes.CopyToContainerOptions) error
	ContainerStart(context.Context, string, containertypes.StartOptions) error
	ContainerStop(context.Context, string, containertypes.StopOptions) error
	ContainerRemove(context.Context, string, containertypes.RemoveOptions) error
} = (*fakeDockerEngine)(nil)
