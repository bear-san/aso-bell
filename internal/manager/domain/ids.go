package domain

const idLength = 24

// IsID は文字列が MongoDB ObjectID の 16 進表現(24 桁)かを返す。
func IsID(s string) bool {
	if len(s) != idLength {
		return false
	}

	for _, r := range s {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'f', r >= 'A' && r <= 'F':
		default:
			return false
		}
	}

	return true
}
