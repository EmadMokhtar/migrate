package dktesting

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/containerd/errdefs"
	"github.com/dhui/dktest"
	"github.com/moby/moby/client"
)

// notReadyErrText is part of the error dktest returns when a container is not ready before
// Options.Timeout. dktest has no sentinel error for this case, so the text is matched.
const notReadyErrText = "timed out waiting for container to get ready"

// maxContainerStarts is how many containers are started for a test before it fails
// because none of them got ready.
const maxContainerStarts = 2

// ContainerSpec holds Docker testing setup specifications
type ContainerSpec struct {
	ImageName string
	Options   dktest.Options
}

// Cleanup cleanups the ContainerSpec after a test run by removing the ContainerSpec's image
func (s *ContainerSpec) Cleanup() (retErr error) {
	// copied from dktest.RunContext()
	dc, err := client.New(client.FromEnv, client.WithAPIVersion("1.41"))
	if err != nil {
		return err
	}
	defer func() {
		if err := dc.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("error closing Docker client: %w", err)
		}
	}()
	cleanupTimeout := s.Options.CleanupTimeout
	if cleanupTimeout <= 0 {
		cleanupTimeout = dktest.DefaultCleanupTimeout
	}
	ctx, timeoutCancelFunc := context.WithTimeout(context.Background(), cleanupTimeout)
	defer timeoutCancelFunc()
	if _, err := dc.ImageRemove(ctx, s.ImageName, client.ImageRemoveOptions{Force: true, PruneChildren: true}); err != nil {
		if errdefs.IsNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

// ParallelTest runs Docker tests in parallel
func ParallelTest(t *testing.T, specs []ContainerSpec,
	testFunc func(*testing.T, dktest.ContainerInfo)) {

	for i, spec := range specs {
		spec := spec // capture range variable, see https://goo.gl/60w3p2

		// Only test against one version in short mode
		// TODO: order is random, maybe always pick first version instead?
		if i > 0 && testing.Short() {
			t.Logf("Skipping %v in short mode", spec.ImageName)
		} else {
			t.Run(spec.ImageName, func(t *testing.T) {
				t.Parallel()
				runWithRetry(t, spec, testFunc)
			})
		}
	}
}

// runWithRetry is like dktest.Run, but it starts a new container if the container never gets ready.
// On busy CI runners a container sometimes stops or hangs while it starts, and a new one usually works.
func runWithRetry(t *testing.T, spec ContainerSpec, testFunc func(*testing.T, dktest.ContainerInfo)) {
	var err error
	for start := 1; start <= maxContainerStarts; start++ {
		err = dktest.RunContext(context.Background(), t, spec.ImageName, spec.Options,
			func(c dktest.ContainerInfo) error {
				testFunc(t, c)
				return nil
			})
		if err == nil || !strings.Contains(err.Error(), notReadyErrText) {
			break
		}
		if start < maxContainerStarts {
			t.Log("Starting a new container because the last one never got ready:", err)
		}
	}
	if err != nil {
		t.Fatal("Failed:", err)
	}
}
