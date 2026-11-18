package main

import (
	"fmt"
	"strconv"
	"strings"

	"benevole-ape/internal/store"
)

// defaultRetentionDays applies when BENEVOLE_RETENTION_DAYS is not set.
const defaultRetentionDays = 60

// defaultMaxRetentionDays is the longest period that an organiser can choose,
// when BENEVOLE_RETENTION_MAX_DAYS is not set.
const defaultMaxRetentionDays = 180

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
// that an organiser can choose. It returns 0 when the variable is empty or
// not set (see newRetention). Values like 0 or "off" are errors: a maximum
// that allows anything would not limit the retention of personal data.
func retentionMaxFromEnv() (int, error) {
	v := strings.TrimSpace(env("BENEVOLE_RETENTION_MAX_DAYS", ""))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("BENEVOLE_RETENTION_MAX_DAYS=%q: use a number of days, at least 1", v)
	}
	return n, nil
}

// newRetention builds the deletion policy from the default number of days
// (0: events are kept forever) and the maximum (0: not set).
//
// When the maximum is not set, it is defaultMaxRetentionDays, or the default
// number of days if that is longer. So a server that already runs with a long
// BENEVOLE_RETENTION_DAYS still starts after an upgrade. The two values are
// refused only when they conflict.
func newRetention(days, maxDays int) (store.Retention, error) {
	switch {
	case days < 0:
		return store.Retention{}, fmt.Errorf("retention days must not be negative (got %d)", days)
	case maxDays < 0:
		return store.Retention{}, fmt.Errorf("maximum retention days must not be negative (got %d)", maxDays)
	case maxDays == 0:
		maxDays = max(defaultMaxRetentionDays, days)
	case days > maxDays:
		return store.Retention{}, fmt.Errorf("retention of %d days is above the maximum of %d days: raise BENEVOLE_RETENTION_MAX_DAYS (-retention-max-days) or lower BENEVOLE_RETENTION_DAYS (-retention-days)", days, maxDays)
	}
	return store.Retention{DefaultDays: days, MaxDays: maxDays}, nil
}
