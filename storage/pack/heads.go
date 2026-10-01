package pack

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/twcclan/goback/backup"
	"github.com/twcclan/goback/proto"
)

// HeadExt names the pointers to each set's latest commit:
// sets/<set id>.head.
const HeadExt = ".head"

// Head is the latest complete commit of a set the store holds, as kept in
// the storage beside the archives, so the set's backups are found without
// any index.
type Head struct {
	SetID        uint64 `json:"setId"`
	Set          string `json:"set"`
	Commit       []byte `json:"commit"`
	ReceivedAtNs int64  `json:"receivedAtNs"`
}

func headName(setID uint64) string {
	return fmt.Sprintf("sets/%d%s", setID, HeadExt)
}

// Heads lists the head of every set that has committed.
func (ps *PackStorage) Heads() ([]Head, error) {
	names, err := ps.storage.List(HeadExt)
	if err != nil {
		return nil, err
	}

	var heads []Head
	for _, name := range names {
		id, err := strconv.ParseUint(strings.TrimSuffix(strings.TrimPrefix(name, "sets/"), HeadExt), 10, 64)
		if err != nil {
			continue
		}

		head, ok, err := ps.readHead(id)
		if err != nil {
			return nil, err
		}

		if ok {
			heads = append(heads, head)
		}
	}

	return heads, nil
}

func (ps *PackStorage) readHead(setID uint64) (Head, bool, error) {
	file, err := ps.storage.Open(headName(setID))
	if notExist(err) {
		return Head{}, false, nil
	}

	if err != nil {
		return Head{}, false, err
	}
	defer file.Close()

	var head Head
	if err := json.NewDecoder(file).Decode(&head); err != nil {
		return Head{}, false, fmt.Errorf("reading the head of set %d: %w", setID, err)
	}

	return head, true, nil
}

var _ backup.HeadKeeper = (*PackStorage)(nil)

// AdvanceHead implements backup.HeadKeeper. Checkpoints never become
// heads.
func (ps *PackStorage) AdvanceHead(object *proto.Object) error {
	commit := object.GetCommit()
	if commit == nil || commit.GetPartial() || commit.GetSetId() == 0 {
		return nil
	}

	current, ok, err := ps.readHead(commit.GetSetId())
	if err != nil {
		return err
	}

	if ok && current.ReceivedAtNs >= commit.GetReceivedAtNs() {
		return nil
	}

	data, err := json.Marshal(Head{SetID: commit.GetSetId(), Set: commit.GetBackupSet(), Commit: object.Ref().Hash, ReceivedAtNs: commit.GetReceivedAtNs()})
	if err != nil {
		return err
	}

	file, err := ps.storage.Create(headName(commit.GetSetId()))
	if err != nil {
		return err
	}

	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}
