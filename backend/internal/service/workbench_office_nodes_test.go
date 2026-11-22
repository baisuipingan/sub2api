package service

import (
	"encoding/json"
	"net/netip"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestOfficeNodeTransportOriginValidation(t *testing.T) {
	for raw, expected := range map[string]string{
		"https://Office.Example.COM.:443/": "https://office.example.com",
		"http://office-tools:8094":         "http://office-tools:8094",
		"http://100.100.10.1:8094":         "http://100.100.10.1:8094",
		"http://[fd00::1]:8094":            "http://[fd00::1]:8094",
	} {
		origin, err := normalizeOfficeNodeOrigin(raw)
		require.NoError(t, err, raw)
		require.Equal(t, expected, origin)
	}
	for _, raw := range []string{"http://1.1.1.1:8094", "https://169.254.169.254", "https://127.0.0.1", "http://localhost", "https://user:secret@node.test", "https://node.test/path", "https://node.test?", "https://node.test#"} {
		_, err := normalizeOfficeNodeOrigin(raw)
		require.ErrorIs(t, err, ErrOfficeNodeInvalid, raw)
	}
	require.True(t, officeNodeAllowedAddress(netip.MustParseAddr("1.1.1.1"), true))
	require.False(t, officeNodeAllowedAddress(netip.MustParseAddr("1.1.1.1"), false))
	require.False(t, officeNodeAllowedAddress(netip.MustParseAddr("::ffff:169.254.169.254"), true))
	require.False(t, officeNodeAllowedAddress(netip.MustParseAddr("100.100.100.200"), true))
	require.False(t, officeNodeAllowedAddress(netip.MustParseAddr("fd00:ec2::254"), true))
}

func TestOfficeNodeSummaryAndTokenVisibility(t *testing.T) {
	now := time.Now()
	status := &OfficeNodeStatus{NodeID: "node", Concurrency: 3, Actions: []string{"read_docx"}, Running: 1, AwaitingUpload: 1}
	active := OfficeNode{State: OfficeNodeActive, Health: OfficeNodeOnline, WorkerIdentity: "node", Status: status, LastCheckedAt: &now, MaxConcurrent: 3, Assigned: 2, TokenEncrypted: "secret-ciphertext"}
	draining := active
	draining.State = OfficeNodeDraining
	stale := active
	old := now.Add(-2 * officeNodeFreshness)
	stale.LastCheckedAt = &old
	summary := summarizeOfficeNodes([]OfficeNode{active, draining, stale})
	require.Equal(t, 3, summary.Total)
	require.Equal(t, 2, summary.Online)
	require.Equal(t, 1, summary.Draining)
	require.Equal(t, 2, summary.Running)
	require.Equal(t, 2, summary.AwaitingUpload)
	require.Equal(t, 6, summary.Assigned)
	require.Equal(t, 1, summary.Slots)
	encoded, err := json.Marshal(active)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-ciphertext")
}

func TestOfficeNodeCapacityDuringConcurrencySync(t *testing.T) {
	node := OfficeNode{MaxConcurrent: 3, Status: &OfficeNodeStatus{
		NodeID: "node", Actions: []string{"read_docx"}, Concurrency: 1,
	}}
	require.Equal(t, 1, node.officeNodeCapacity())
	var revision int64
	node.Status.ConcurrencyRevision = &revision
	require.Zero(t, node.officeNodeCapacity())

	revision = 2
	node.Status.Concurrency = 3
	node.Status.Running = 3
	require.Equal(t, 3, node.officeNodeCapacity())
	node.MaxConcurrent = 1
	require.Equal(t, 1, node.officeNodeCapacity())
	node.Status.Concurrency = 1
	require.True(t, validOfficeNodeStatus(*node.Status), "tasks started under the higher limit are still running")
	node.Status.ConcurrencyRevision = nil
	require.False(t, validOfficeNodeStatus(*node.Status), "legacy nodes have a fixed worker limit")
}
