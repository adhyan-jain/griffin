package scheduler

import (
	"errors"
	"fmt"
	"github.com/robfig/cron/v3"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	ErrUnknownCadence = errors.New("scheduler: unknown cadence format")
	istZone           = time.FixedZone("IST", 5*3600+1800)
)

var (
	dailyMultiRe = regexp.MustCompile(`(?i)^daily\s+(\d{1,2}):(\d{2})\s+and\s+(\d{1,2}):(\d{2})(?:\s+([A-Za-z/_]+))?$`)
	dailyRe      = regexp.MustCompile(`(?i)^daily\s+(\d{1,2}):(\d{2})(?:\s+([A-Za-z/_]+))?$`)
	weekdayRe    = regexp.MustCompile(`(?i)^(?:weekly\s+)?(monday|tuesday|wednesday|thursday|friday|saturday|sunday|mon|tue|wed|thu|fri|sat|sun)\s+(\d{1,2}):(\d{2})(?:\s+([A-Za-z/_]+))?$`)
	monthlyRe    = regexp.MustCompile(`(?i)^monthly(?:\s+1st)?(?:\s+(\d{1,2}):(\d{2}))?(?:\s+([A-Za-z/_]+))?$`)
	everyHoursRe = regexp.MustCompile(`(?i)^every\s+(\d+)\s+hours?$`)
	everyMinsRe  = regexp.MustCompile(`(?i)^every\s+(\d+)\s+(?:mins?|minutes?)$`)
)

func parseWeekday(s string) (time.Weekday, bool) {
	switch strings.ToLower(s) {
	case "sunday", "sun":
		return time.Sunday, true
	case "monday", "mon":
		return time.Monday, true
	case "tuesday", "tue":
		return time.Tuesday, true
	case "wednesday", "wed":
		return time.Wednesday, true
	case "thursday", "thu":
		return time.Thursday, true
	case "friday", "fri":
		return time.Friday, true
	case "saturday", "sat":
		return time.Saturday, true
	default:
		return 0, false
	}
}

func resolveLocation(tz string) *time.Location {
	tz = strings.ToUpper(strings.TrimSpace(tz))
	switch tz {
	case "IST", "":
		return istZone
	case "UTC":
		return time.UTC
	default:
		if loc, err := time.LoadLocation(tz); err == nil {
			return loc
		}
		return istZone
	}
}

// NextOccurrence calculates the next future execution time strictly after `after` for a given cadence string.
func NextOccurrence(cadence string, after time.Time) (time.Time, error) {
	cadence = strings.TrimSpace(cadence)
	if cadence == "" {
		return time.Time{}, errors.New("scheduler: empty cadence")
	}

	// 1. "daily HH:MM and HH:MM [TZ]"
	if m := dailyMultiRe.FindStringSubmatch(cadence); m != nil {
		h1, _ := strconv.Atoi(m[1])
		min1, _ := strconv.Atoi(m[2])
		h2, _ := strconv.Atoi(m[3])
		min2, _ := strconv.Atoi(m[4])
		loc := resolveLocation(m[5])

		t1 := nextDailyTime(after.In(loc), h1, min1)
		t2 := nextDailyTime(after.In(loc), h2, min2)
		if t1.Before(t2) {
			return t1.UTC(), nil
		}
		return t2.UTC(), nil
	}

	// 2. "daily HH:MM [TZ]"
	if m := dailyRe.FindStringSubmatch(cadence); m != nil {
		h, _ := strconv.Atoi(m[1])
		min, _ := strconv.Atoi(m[2])
		loc := resolveLocation(m[3])
		return nextDailyTime(after.In(loc), h, min).UTC(), nil
	}

	// 3. "<Weekday> HH:MM [TZ]" or "weekly <Weekday> HH:MM [TZ]"
	if m := weekdayRe.FindStringSubmatch(cadence); m != nil {
		wd, _ := parseWeekday(m[1])
		h, _ := strconv.Atoi(m[2])
		min, _ := strconv.Atoi(m[3])
		loc := resolveLocation(m[4])
		return nextWeekdayTime(after.In(loc), wd, h, min).UTC(), nil
	}

	// 4. "monthly" or "monthly 1st 10:00 IST"
	if m := monthlyRe.FindStringSubmatch(cadence); m != nil {
		h := 10
		min := 0
		if m[1] != "" {
			h, _ = strconv.Atoi(m[1])
			min, _ = strconv.Atoi(m[2])
		}
		loc := resolveLocation(m[3])
		return nextMonthlyTime(after.In(loc), 1, h, min).UTC(), nil
	}

	// 5. "every N hours"
	if m := everyHoursRe.FindStringSubmatch(cadence); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n <= 0 {
			n = 1
		}
		return after.Add(time.Duration(n) * time.Hour).UTC(), nil
	}

	// 6. "every N minutes"
	if m := everyMinsRe.FindStringSubmatch(cadence); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n <= 0 {
			n = 1
		}
		return after.Add(time.Duration(n) * time.Minute).UTC(), nil
	}

	// 7. A standard 5-field cron expression ("45 7 * * *"), as the schedules table stores. Times are IST.
	if len(strings.Fields(cadence)) == 5 {
		if sched, err := cron.ParseStandard("CRON_TZ=Asia/Kolkata " + cadence); err == nil {
			return sched.Next(after).UTC(), nil
		}
	}

	return time.Time{}, fmt.Errorf("%w: %q", ErrUnknownCadence, cadence)
}

func nextDailyTime(ref time.Time, hour, minute int) time.Time {
	target := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, minute, 0, 0, ref.Location())
	if !target.After(ref) {
		target = target.AddDate(0, 0, 1)
	}
	return target
}

func nextWeekdayTime(ref time.Time, targetDay time.Weekday, hour, minute int) time.Time {
	daysAhead := int(targetDay - ref.Weekday())
	if daysAhead < 0 {
		daysAhead += 7
	}
	target := time.Date(ref.Year(), ref.Month(), ref.Day(), hour, minute, 0, 0, ref.Location()).AddDate(0, 0, daysAhead)
	if !target.After(ref) {
		target = target.AddDate(0, 0, 7)
	}
	return target
}

func nextMonthlyTime(ref time.Time, day, hour, minute int) time.Time {
	target := time.Date(ref.Year(), ref.Month(), day, hour, minute, 0, 0, ref.Location())
	if !target.After(ref) {
		target = time.Date(ref.Year(), ref.Month()+1, day, hour, minute, 0, 0, ref.Location())
	}
	return target
}
