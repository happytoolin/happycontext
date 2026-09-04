package hc

// Level is the event severity, int-backed with slog-compatible ranks.
// The wire format is unchanged: sinks render the same names they emitted
// in v0 (lowercase on the JSON wire, uppercase from String).
type Level int

const (
	LevelDebug Level = -4
	LevelInfo  Level = 0
	LevelWarn  Level = 4
	LevelError Level = 8
)

// String renders the classic level names.
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// IsValidLevel reports whether level is one of the four defined levels.
func IsValidLevel(level Level) bool {
	switch level {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
		return true
	default:
		return false
	}
}

// levelFloor returns the more severe of auto and requested (when set);
// the internal successor of v0's MergeLevelWithFloor.
func levelFloor(auto Level, requested Level, hasRequested bool) Level {
	if !hasRequested || !IsValidLevel(requested) {
		return auto
	}
	return max(auto, requested)
}
