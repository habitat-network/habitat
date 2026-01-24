package utils

import (
	"os"
	"strings"
)

func GetEnvString(key string, defaultVal string) string {
	// TODO: possibly this should handle upper and lower envs?
	val := os.Getenv(strings.ToUpper(key))
	if val == "" {
		return defaultVal
	}
	return val
}
