package utils

func RemoveString(slice []string, s string) []string {
	out := make([]string, 0, len(slice))
	for _, v := range slice {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}

func ContainsString(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}
