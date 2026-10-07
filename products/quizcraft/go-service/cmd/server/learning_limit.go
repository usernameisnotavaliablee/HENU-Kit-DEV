package main

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// learningManualLimitDefault is deliberately small: a member needs a handful of
// generations per course per hour at most, and a script must not be able to buy
// unlimited model work. It is tuning, not a product quota.
const learningManualLimitDefault = 10

const learningManualLimitMax = 1000

// learningManualLimitFromEnv reads the per-member, per-course abuse guard for
// member-requested course feedback. Empty means the documented default; "0"
// turns the guard off explicitly (an operator decision, logged as such).
func learningManualLimitFromEnv() (int, error) {
	value := strings.TrimSpace(os.Getenv("QUIZCRAFT_LEARNING_MANUAL_LIMIT"))
	if value == "" {
		return learningManualLimitDefault, nil
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 || limit > learningManualLimitMax {
		return 0, errors.New("QUIZCRAFT_LEARNING_MANUAL_LIMIT must be 0 (disabled) or 1..1000")
	}
	return limit, nil
}
