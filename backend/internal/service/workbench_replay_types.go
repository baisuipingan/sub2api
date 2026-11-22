package service

import "encoding/json"

// OfficeReplayTurn orders answer variants by their original user turn.
type OfficeReplayTurn struct {
	Sequence        int64
	RunID           string
	GroupID         int64
	Model           string
	Parameters      json.RawMessage
	SearchEnabled   bool
	User            OfficeMessage
	Assistant       *OfficeMessage
	NativeAvailable bool
	NativeFrom      int64
	CheckpointID    string
}

// OfficeReplayCheckpoint contains a bounded summary of turns preceding
// BeforeSequence. The boundary is the first retained logical turn.
type OfficeReplayCheckpoint struct {
	ID             string
	BeforeSequence int64
	Content        json.RawMessage
}

type OfficeReplayState struct {
	Turns      []OfficeReplayTurn
	Checkpoint *OfficeReplayCheckpoint
	NativeFrom int64
	Pinned     bool
	Truncated  bool
}
