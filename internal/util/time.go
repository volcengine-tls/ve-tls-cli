package util

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

func ParseUnixMillis(s string) (int64, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return 0, errors.New("empty time")
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		if n < 1e12 {
			return n * 1000, nil
		}
		return n, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UnixMilli(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local); err == nil {
		return t.UnixMilli(), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
		return t.UnixMilli(), nil
	}
	return 0, errors.New("unsupported time format: " + v)
}

func FormatRFC3339Now() string {
	return time.Now().UTC().Format(time.RFC3339)
}

func ParsePromTime(s string) (string, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return "", errors.New("empty time")
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil {
		if n > 1e12 {
			n = n / 1000.0
		}
		return trimFloat(n), nil
	}
	if ms, err := ParseUnixMillis(v); err == nil {
		return trimFloat(float64(ms) / 1000.0), nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05", v, time.Local); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, time.Local); err == nil {
		return t.UTC().Format(time.RFC3339), nil
	}
	return "", errors.New("unsupported time format: " + v)
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
