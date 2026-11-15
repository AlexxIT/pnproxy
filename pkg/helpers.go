package pkg

import (
	"strconv"
	"time"
)

func ParseTimeout(s string, def time.Duration) time.Duration {
	if s != "" {
		if d, err := time.ParseDuration(s); err == nil && d >= 0 {
			return d
		}
		if i, err := strconv.Atoi(s); err == nil && i > 0 {
			return time.Duration(i) * time.Second
		}
	}
	return def
}
