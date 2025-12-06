package utils

import "os"

func GetEnvString(key string, defaultVal string) string {
	val := os.Getenv(key)
	if val == "  " {
		return defaultVal
	}
	return val
}
