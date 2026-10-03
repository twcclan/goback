package common

import (
	"encoding/hex"
	"time"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

// Hex is a ref as the hex its hash prints as, empty for none.
func Hex(ref *proto.Ref) string {
	if ref == nil {
		return ""
	}

	return hex.EncodeToString(ref.Hash)
}

// CommitView is a commit as JSON output shows it.
type CommitView struct {
	Ref        string            `json:"ref"`
	Tree       string            `json:"tree"`
	Parent     string            `json:"parent,omitempty"`
	Set        string            `json:"set"`
	Agent      string            `json:"agent,omitempty"`
	Time       time.Time         `json:"time"`
	Consistent bool              `json:"consistent"`
	Partial    bool              `json:"partial,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

// ViewCommit is commit as JSON output shows it.
func ViewCommit(commit *proto.Commit) CommitView {
	return CommitView{Ref: Hex(proto.NewObject(commit).Ref()), Tree: Hex(commit.GetTree()), Parent: Hex(commit.GetParent()),
		Set: commit.GetBackupSet(), Agent: commit.GetAgentId(), Time: time.Unix(commit.GetTimestamp(), 0).UTC(),
		Consistent: commit.GetConsistent(), Partial: commit.GetPartial(), Metadata: commit.GetMetadata()}
}

// NodeView is a file or directory as JSON output shows it.
type NodeView struct {
	Name     string    `json:"name"`
	Type     string    `json:"type"`
	Size     int64     `json:"size"`
	Mode     uint32    `json:"mode"`
	Modified time.Time `json:"modified"`
	Target   string    `json:"target,omitempty"`
	Ref      string    `json:"ref,omitempty"`
}

// ViewNode is node as JSON output shows it.
func ViewNode(node *proto.TreeNode) NodeView {
	info := node.GetStat()
	kind := "file"

	switch info.GetType() {
	case proto.NodeType_NODE_DIRECTORY:
		kind = "directory"
	case proto.NodeType_NODE_SYMLINK:
		kind = "symlink"
	}

	return NodeView{Name: string(info.GetName()), Type: kind, Size: info.GetSize(), Mode: info.GetMode(),
		Modified: info.ModTime().UTC(), Target: string(info.GetLinkTarget()), Ref: Hex(node.GetRef())}
}

// RestoreStatsView is what a restore did, as JSON output shows it.
type RestoreStatsView struct {
	Files                int64 `json:"files"`
	Written              int64 `json:"written"`
	Unchanged            int64 `json:"unchanged"`
	Skipped              int64 `json:"skipped"`
	Salvaged             int64 `json:"salvaged"`
	MissingBytes         int64 `json:"missing_bytes"`
	BytesFromDestination int64 `json:"bytes_from_destination"`
	BytesFromSeeds       int64 `json:"bytes_from_seeds"`
	BytesFromCache       int64 `json:"bytes_from_cache"`
	BytesFromStore       int64 `json:"bytes_from_store"`
}

// ViewRestoreStats is stats as JSON output shows them.
func ViewRestoreStats(stats backup.RestoreStats) RestoreStatsView {
	return RestoreStatsView(stats)
}

// WalkView is what a backup run did, as JSON output shows it.
type WalkView struct {
	Commit      string `json:"commit"`
	Base        string `json:"base,omitempty"`
	Files       int64  `json:"files"`
	Bytes       int64  `json:"bytes"`
	Uploaded    int64  `json:"uploaded"`
	Reused      int64  `json:"reused"`
	Read        int64  `json:"read"`
	Torn        int64  `json:"torn"`
	Unreadable  int64  `json:"unreadable"`
	Skipped     int64  `json:"skipped"`
	Checkpoints int64  `json:"checkpoints"`
	Assumed     int64  `json:"assumed"`
	Repaired    int64  `json:"repaired"`
}

// ViewWalk is result as JSON output shows it.
func ViewWalk(result *backup.WalkResult) WalkView {
	return WalkView{Commit: Hex(result.Ref), Base: Hex(result.Base), Files: result.Files, Bytes: result.Bytes,
		Uploaded: result.Uploaded, Reused: result.Reused, Read: result.Read, Torn: result.Torn, Unreadable: result.Unreadable,
		Skipped: result.Skipped, Checkpoints: result.Checkpoints, Assumed: result.Assumed, Repaired: result.Repaired}
}

// Done is the result of a command that changed something and has nothing
// more to say than what it changed.
type Done struct {
	Action string `json:"action"`
	Ref    string `json:"ref,omitempty"`
	Name   string `json:"name,omitempty"`
}
