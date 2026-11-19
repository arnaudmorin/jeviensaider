package main

import (
	"fmt"
	"strconv"
	"strings"
)

// defaultRetentionDays applies when BENEVOLE_RETENTION_DAYS is not set.
const defaultRetentionDays = 60

// retentionFromEnv reads BENEVOLE_RETENTION_DAYS: a number of days, or 0,
// "off", "never" or "forever" to keep events forever. Anything else is an
// error: silently falling back to the default would delete events on an
// instance that meant to keep them.
func retentionFromEnv() (int, error) {
	v := strings.ToLower(strings.TrimSpace(env("BENEVOLE_RETENTION_DAYS", "")))
	switch v {
	case "":
		return defaultRetentionDays, nil
	case "0", "off", "never", "forever", "false", "no", "none":
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("BENEVOLE_RETENTION_DAYS=%q: use a number of days, or 0 / off to keep events forever", v)
	}
	return n, nil
}

// retentionMaxFromEnv reads BENEVOLE_RETENTION_MAX_DAYS, the longest period
// that an organiser can choose. Empty, unset or 0 means the default maximum
// (see store.NewRetention), like the -retention-max-days flag.
func retentionMaxFromEnv() (int, error) {
	v := strings.TrimSpace(env("BENEVOLE_RETENTION_MAX_DAYS", ""))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("BENEVOLE_RETENTION_MAX_DAYS=%q: use a number of days, or 0 for the default maximum", v)
	}
	return n, nil
}
