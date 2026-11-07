package service

import (
	"os"
	"os/exec"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

// runAllocationTestInSubprocess runs only the current test in a fresh process
// and returns true in the parent. AllocsPerRun reads process-wide allocation
// counters, which background workers from other service tests can contaminate.
// The child returns false so the test can enforce its original allocation bound.
func runAllocationTestInSubprocess(t *testing.T) bool {
	t.Helper()
	const childEnv = "SUB2API_ISOLATED_ALLOCATION_TEST"
	if os.Getenv(childEnv) == t.Name() {
		return false
	}
	binary, err := os.Executable()
	require.NoError(t, err)
	child := exec.Command(binary, "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.count=1", "-test.timeout=1m")
	child.Env = append(os.Environ(), childEnv+"="+t.Name())
	output, err := child.CombinedOutput()
	require.NoError(t, err, "isolated allocation test failed: %s", output)
	return true
}
