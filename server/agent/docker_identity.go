package agent

import (
	"fmt"
	"os/user"
	"path"
	"runtime"
	"strconv"
)

type dockerCurrentUserFunc func() (*user.User, error)

func currentDockerRuntimeUser() (string, error) {
	return resolveDockerRuntimeUser(runtime.GOOS, user.Current)
}

func resolveDockerRuntimeUser(goos string, current dockerCurrentUserFunc) (string, error) {
	if goos == "windows" {
		// Docker Desktop mediates Windows bind-mount ACLs. Keep the image identity
		// rather than attempting to map Windows account identifiers into Linux IDs.
		return ContainerRuntimeUser, nil
	}
	currentUser, err := current()
	if err != nil {
		return "", fmt.Errorf("resolve current host user for Docker: %w", err)
	}
	uid, err := strictDockerDecimalID("UID", currentUser.Uid)
	if err != nil {
		return "", err
	}
	gid, err := strictDockerDecimalID("GID", currentUser.Gid)
	if err != nil {
		return "", err
	}
	if uid == 0 {
		return "", fmt.Errorf("local Docker bind mounts require the server to run as a non-root or rootless user; host UID 0 is not allowed")
	}
	return fmt.Sprintf("%d:%d", uid, gid), nil
}

func strictDockerDecimalID(label, value string) (uint64, error) {
	if value == "" {
		return 0, fmt.Errorf("host %s is empty", label)
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return 0, fmt.Errorf("host %s %q is not a decimal numeric ID", label, value)
		}
	}
	parsed, err := strconv.ParseUint(value, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("parse host %s %q: %w", label, value, err)
	}
	return parsed, nil
}

func dockerRuntimeHome(workDirInContainer string) string {
	return path.Join(workDirInContainer, DockerRuntimeHomeDirName)
}
