package session

import (
	"encoding/json"
	"errors"
	"fmt"
)

// recordVersion is the format version of the records written to the store.
const recordVersion = 1

var errRecordVersion = errors.New("session: unknown record format version")

// sessionRecord is the store value of a session. Times are Unix seconds. Gen
// is the revocation state of the subject when the session was issued.
type sessionRecord struct {
	V   int      `json:"v"`
	Sub string   `json:"sub"`
	AMR []string `json:"amr"`
	IAT int64    `json:"iat"`
	EXP int64    `json:"exp"`
	RW  int64    `json:"rw"`
	Gen string   `json:"gen,omitempty"`
}

// pendingRecord is the store value of a pending login. Gen is as in
// sessionRecord.
type pendingRecord struct {
	V   int      `json:"v"`
	Sub string   `json:"sub"`
	AMR []string `json:"amr"`
	IAT int64    `json:"iat"`
	Gen string   `json:"gen,omitempty"`
}

func encodeRecord(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("session: encode record: %w", err)
	}
	return b, nil
}

// decodeRecord decodes a record into dst, a *sessionRecord or a
// *pendingRecord, and rejects unknown versions.
func decodeRecord(b []byte, dst any) error {
	var head struct {
		V int `json:"v"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return fmt.Errorf("session: decode record: %w", err)
	}
	if head.V != recordVersion {
		return errRecordVersion
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("session: decode record: %w", err)
	}
	return nil
}
