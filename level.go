package hc

// IsValidLevel reports whether level is a valid severity level.
func IsValidLevel(level Level) bool {
	switch level {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
		return true
	default:
		return false
	}
}

// LevelRank returns the numeric severity rank for a level.
// Higher values indicate more severe levels.
func LevelRank(level Level) int {
	switch level {
	case LevelDebug:
		return 10
	case LevelInfo:
		return 20
	case LevelWarn:
		return 30
	case LevelError:
		return 40
	default:
		return 20
	}
}

// MergeLevelWithFloor merges an automatically chosen level with an optional requested floor.
func MergeLevelWithFloor(autoLevel, requestedLevel Level, hasRequested bool) Level {
	if !hasRequested || !IsValidLevel(requestedLevel) {
		return autoLevel
	}
	if LevelRank(requestedLevel) > LevelRank(autoLevel) {
		return requestedLevel
	}
	return autoLevel
}
