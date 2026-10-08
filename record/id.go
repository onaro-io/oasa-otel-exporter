// Copyright Onaro (BrianOnAI LLC)
// SPDX-License-Identifier: Apache-2.0

package record

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// DeterministicID derives a UUIDv7 record_id from occurred_at and
// source_record_id, following SPEC.md "Deterministic record IDs":
// the 48-bit timestamp is occurred_at in Unix milliseconds (truncated), and
// the remaining bits come from SHA-256 of source_record_id. The same inputs
// always produce the same ID, so a retried export does not create a duplicate.
func DeterministicID(occurredAt time.Time, sourceRecordID string) string {
	ms := uint64(occurredAt.UnixMilli())
	h := sha256.Sum256([]byte(sourceRecordID))

	var b [16]byte
	b[0] = byte(ms >> 40)
	b[1] = byte(ms >> 32)
	b[2] = byte(ms >> 24)
	b[3] = byte(ms >> 16)
	b[4] = byte(ms >> 8)
	b[5] = byte(ms)
	b[6] = 0x70 | (h[0] & 0x0f)
	b[7] = h[1]
	b[8] = 0x80 | (h[2] & 0x3f)
	copy(b[9:], h[3:10])

	var out [36]byte
	hex.Encode(out[0:8], b[0:4])
	out[8] = '-'
	hex.Encode(out[9:13], b[4:6])
	out[13] = '-'
	hex.Encode(out[14:18], b[6:8])
	out[18] = '-'
	hex.Encode(out[19:23], b[8:10])
	out[23] = '-'
	hex.Encode(out[24:], b[10:])
	return string(out[:])
}
