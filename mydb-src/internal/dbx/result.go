package dbx

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

// ValueKind tags a cell so the grid can style NULLs, numbers, and byte
// previews differently.
type ValueKind int

const (
	VNull ValueKind = iota
	VText
	VNumber
	VBool
	VBytes
	VTime
)

// Value is one pre-rendered cell. Drivers stringify engine-native values
// once, off the update loop; the UI only ever handles strings.
type Value struct {
	Kind ValueKind
	S    string
}

// Column describes one result column.
type Column struct {
	Name     string
	TypeName string
}

// Result is a page of rows (data viewer) or a script's output (query
// runner). For multi-statement scripts it carries the LAST result set
// plus the statement count and total affected rows (spec v1 decision).
type Result struct {
	Columns   []Column
	Rows      [][]Value
	Affected  int64
	Stmts     int // statements executed (0 for page reads)
	Elapsed   time.Duration
	Truncated bool // more rows exist beyond the requested cap
}

// PageReq asks for one stable-ordered page of a relation. Offset paging
// in v1; a keyset cursor slots in here later without interface changes.
type PageReq struct {
	Offset int
	Limit  int
}

// QueryReq bounds a script execution.
type QueryReq struct {
	MaxRows int // cap on returned rows; Result.Truncated marks overflow
}

// cellByteCap bounds how much of a huge cell is stringified; the rest is
// elided with a byte count so a BLOB column cannot bloat a frame.
const cellByteCap = 4096

// Render converts an engine-native value into a cell. Drivers share it
// so NULL/bytes/time handling stays identical across engines.
func Render(v any) Value {
	switch x := v.(type) {
	case nil:
		return Value{Kind: VNull}
	case bool:
		return Value{Kind: VBool, S: strconv.FormatBool(x)}
	case int64:
		return Value{Kind: VNumber, S: strconv.FormatInt(x, 10)}
	case int32:
		return Value{Kind: VNumber, S: strconv.FormatInt(int64(x), 10)}
	case int16:
		return Value{Kind: VNumber, S: strconv.FormatInt(int64(x), 10)}
	case int:
		return Value{Kind: VNumber, S: strconv.Itoa(x)}
	case float64:
		return Value{Kind: VNumber, S: strconv.FormatFloat(x, 'g', -1, 64)}
	case float32:
		return Value{Kind: VNumber, S: strconv.FormatFloat(float64(x), 'g', -1, 32)}
	case time.Time:
		return Value{Kind: VTime, S: x.Format("2006-01-02 15:04:05.999999-07")}
	case []byte:
		if len(x) > cellByteCap {
			return Value{Kind: VBytes, S: fmt.Sprintf("0x%s… (%d bytes)",
				hex.EncodeToString(x[:32]), len(x))}
		}
		return Value{Kind: VBytes, S: "0x" + hex.EncodeToString(x)}
	case string:
		if len(x) > cellByteCap {
			return Value{Kind: VText, S: x[:cellByteCap] + fmt.Sprintf("… (%d bytes)", len(x))}
		}
		return Value{Kind: VText, S: x}
	default:
		s := fmt.Sprint(x)
		if len(s) > cellByteCap {
			s = s[:cellByteCap] + "…"
		}
		return Value{Kind: VText, S: s}
	}
}
