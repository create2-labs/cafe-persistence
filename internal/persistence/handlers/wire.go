package handlers

import (
	"encoding/json"

	lifecycle "github.com/create2-labs/cafe-contracts/discovery/scanlifecycle/v1"
	"github.com/google/uuid"
)

// Storage view of a contract message. JSON stays in discovery/scanlifecycle/v1.
// UUIDs are parsed for the Postgres and Redis writers.
type scanStarted struct {
	ScanID   uuid.UUID
	Kind     string
	UserID   uuid.UUID
	Endpoint string
	Address  string
}

type scanCompleted struct {
	ScanID   uuid.UUID
	Kind     string
	UserID   uuid.UUID
	Endpoint string
	Address  string
	Result   any
}

type scanFailed struct {
	ScanID   uuid.UUID
	Kind     string
	UserID   uuid.UUID
	Error    string
	Endpoint string
	Address  string
}

func wireIDs(scanID, userID string) (uuid.UUID, uuid.UUID, error) {
	sid, err := uuid.Parse(scanID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	uid, err := uuid.Parse(userID)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return sid, uid, nil
}

func (h *ScanEventHandler) HandleStarted(msg *lifecycle.Started) error {
	view, err := newScanStarted(msg)
	if err != nil {
		return err
	}
	return h.applyStarted(view)
}

func (h *ScanEventHandler) HandleCompleted(msg *lifecycle.Completed) error {
	view, err := newScanCompleted(msg)
	if err != nil {
		return err
	}
	return h.applyCompleted(view)
}

func (h *ScanEventHandler) HandleFailed(msg *lifecycle.Failed) error {
	view, err := newScanFailed(msg)
	if err != nil {
		return err
	}
	return h.applyFailed(view)
}

func newScanStarted(msg *lifecycle.Started) (*scanStarted, error) {
	scanID, userID, err := wireIDs(msg.ScanID, msg.UserID)
	if err != nil {
		return nil, err
	}
	return &scanStarted{
		ScanID: scanID, Kind: msg.Kind, UserID: userID,
		Endpoint: msg.Endpoint, Address: msg.Address,
	}, nil
}

func newScanCompleted(msg *lifecycle.Completed) (*scanCompleted, error) {
	scanID, userID, err := wireIDs(msg.ScanID, msg.UserID)
	if err != nil {
		return nil, err
	}
	return &scanCompleted{
		ScanID: scanID, Kind: msg.Kind, UserID: userID,
		Endpoint: msg.Endpoint, Address: msg.Address, Result: rawValue(msg.Result),
	}, nil
}

func newScanFailed(msg *lifecycle.Failed) (*scanFailed, error) {
	scanID, userID, err := wireIDs(msg.ScanID, msg.UserID)
	if err != nil {
		return nil, err
	}
	return &scanFailed{
		ScanID: scanID, Kind: msg.Kind, UserID: userID, Error: msg.Error,
		Endpoint: msg.Endpoint, Address: msg.Address,
	}, nil
}

func rawValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return raw
}
