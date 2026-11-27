package domain

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OwnerDayStartsAt is the hour (IST) at which the owner's day starts. Before it, the owner is still up and
// "tomorrow" means the coming morning: the calendar date they are in.
const OwnerDayStartsAt = 5

// ISTZone is the owner's time zone.
var ISTZone = time.FixedZone("IST", 5*3600+1800)

// ReminderTitlePrefix starts the title of every reminder task.
const ReminderTitlePrefix = "REMINDER - "

// IsReminder reports whether a task title is a reminder.
func IsReminder(title string) bool { return strings.HasPrefix(title, ReminderTitlePrefix) }

const monthNames = `jan(?:uary)?|feb(?:ruary)?|mar(?:ch)?|apr(?:il)?|may|june?|july?|aug(?:ust)?|sep(?:t(?:ember)?)?|oct(?:ober)?|nov(?:ember)?|dec(?:ember)?`

var (
	reRelHalf   = regexp.MustCompile(`\bin\s+half\s+an?\s+hour\b`)
	reRelAn     = regexp.MustCompile(`\bin\s+an?\s+(hour|minute)\b`)
	reRelN      = regexp.MustCompile(`\bin\s+(\d+)\s*(minutes?|mins?|hours?|hrs?)\b`)
	reDayAfter  = regexp.MustCompile(`\b(?:on\s+)?day\s+after\s+tomorrow\b`)
	reTomorrow  = regexp.MustCompile(`\b(?:on\s+)?tomorrow\b`)
	reToday     = regexp.MustCompile(`\b(?:on\s+)?(today|tonight)\b`)
	reWeekday   = regexp.MustCompile(`\b(?:on\s+|next\s+|this\s+)?(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b`)
	reDateDMon  = regexp.MustCompile(`\b(?:on\s+)?(\d{1,2})(?:st|nd|rd|th)?\s+(?:of\s+)?(` + monthNames + `)\b`)
	reDateMonD  = regexp.MustCompile(`\b(?:on\s+)?(` + monthNames + `)\s+(\d{1,2})(?:st|nd|rd|th)?\b`)
	reDateSlash = regexp.MustCompile(`\b(?:on\s+)?(\d{1,2})/(\d{1,2})\b`)
	reClockAt   = regexp.MustCompile(`\b(?:at|by|around|@)\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm|a\.m\.|p\.m\.)?`)
	reClockAP   = regexp.MustCompile(`\b(\d{1,2})(?::(\d{2}))?\s*(am|pm|a\.m\.|p\.m\.)`)
	reClock24   = regexp.MustCompile(`\b(\d{1,2}):(\d{2})\b`)
	rePartWord  = regexp.MustCompile(`\b(?:(?:in|during|at)\s+the\s+|this\s+|at\s+)?(morning|afternoon|evening|night|noon|midnight)\b`)
	reTonight   = regexp.MustCompile(`\btonight\b`)
)

var weekdays = map[string]time.Weekday{"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6}

var months = map[string]time.Month{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

func monthOf(s string) time.Month { return months[s[:3]] }

// ResolveWhen finds a time phrase in free text and returns the absolute time (IST). It returns false when the
// text has no usable time, when the time is in the past, or when it names a day but no time (the caller asks).
func ResolveWhen(now time.Time, text string) (time.Time, bool) {
	now = now.In(ISTZone)
	s := strings.ToLower(text)

	// Relative: "in 10 minutes", "in an hour", "in half an hour".
	if reRelHalf.MatchString(s) {
		return now.Add(30 * time.Minute), true
	}
	if m := reRelAn.FindStringSubmatch(s); m != nil {
		if m[1] == "hour" {
			return now.Add(time.Hour), true
		}
		return now.Add(time.Minute), true
	}
	if m := reRelN.FindStringSubmatch(s); m != nil {
		n, _ := strconv.Atoi(m[1])
		if n <= 0 {
			return time.Time{}, false
		}
		if strings.HasPrefix(m[2], "h") {
			return now.Add(time.Duration(n) * time.Hour), true
		}
		return now.Add(time.Duration(n) * time.Minute), true
	}

	year, month, day := now.Date()
	today := time.Date(year, month, day, 0, 0, 0, 0, ISTZone)
	night := now.Hour() < OwnerDayStartsAt
	tomorrow := today.AddDate(0, 0, 1)
	if night {
		tomorrow = today // the owner's day has not ended: "tomorrow" is the coming morning
	}

	var date time.Time
	haveDay := true
	switch {
	case reDayAfter.MatchString(s):
		date = tomorrow.AddDate(0, 0, 1)
	case reTomorrow.MatchString(s):
		date = tomorrow
	case reToday.MatchString(s):
		date = today
	case reWeekday.MatchString(s):
		wd := weekdays[reWeekday.FindStringSubmatch(s)[1]]
		date = today.AddDate(0, 0, (int(wd)-int(today.Weekday())+6)%7+1)
	default:
		d, mo, ok := 0, time.Month(0), false
		if m := reDateDMon.FindStringSubmatch(s); m != nil {
			d, _ = strconv.Atoi(m[1])
			mo, ok = monthOf(m[2]), true
		} else if m := reDateMonD.FindStringSubmatch(s); m != nil {
			d, _ = strconv.Atoi(m[2])
			mo, ok = monthOf(m[1]), true
		} else if m := reDateSlash.FindStringSubmatch(s); m != nil {
			d, _ = strconv.Atoi(m[1])
			mi, _ := strconv.Atoi(m[2])
			mo, ok = time.Month(mi), mi >= 1 && mi <= 12
		}
		if ok && d >= 1 && d <= 31 {
			date = time.Date(year, mo, d, 0, 0, 0, 0, ISTZone)
			if date.Day() != d {
				return time.Time{}, false // "31 nov" does not exist
			}
			if date.Before(today) {
				date = date.AddDate(1, 0, 0)
			}
		} else {
			haveDay = false
		}
	}

	hh, mm, haveTime, ambiguous := clockOf(s)
	if !haveTime {
		return time.Time{}, false // a day with no time: ask, never guess
	}
	if !haveDay {
		date = today
	}
	at := time.Date(date.Year(), date.Month(), date.Day(), hh, mm, 0, 0, ISTZone)
	if pm := at.Add(12 * time.Hour); !at.After(now) && !haveDay && ambiguous && pm.After(now) {
		at = pm // "at 9" said at 20:00: the owner means 9 pm, not 9 am tomorrow
	}
	if !at.After(now) {
		if haveDay {
			return time.Time{}, false
		}
		at = at.AddDate(0, 0, 1)
	}
	return at, true
}

// clockOf reads the clock time in lower-cased text: a number with am/pm, "at 5", "17:30", or a word like
// "noon" or "evening".
func clockOf(s string) (hh, mm int, ok, ambiguous bool) {
	word := ""
	if m := rePartWord.FindStringSubmatch(s); m != nil {
		word = m[1]
	}
	if reTonight.MatchString(s) && word == "" {
		word = "night"
	}
	var m []string
	for _, re := range []*regexp.Regexp{reClockAP, reClockAt, reClock24} {
		if m = re.FindStringSubmatch(s); m != nil {
			break
		}
	}
	if m == nil {
		switch word {
		case "morning":
			return 9, 0, true, false
		case "noon":
			return 12, 0, true, false
		case "afternoon":
			return 14, 0, true, false
		case "evening":
			return 19, 0, true, false
		case "night":
			return 21, 0, true, false
		case "midnight":
			return 0, 0, true, false
		}
		return 0, 0, false, false
	}
	h, _ := strconv.Atoi(m[1])
	if m[2] != "" {
		mm, _ = strconv.Atoi(m[2])
	}
	ap := ""
	if len(m) > 3 {
		ap = strings.ReplaceAll(m[3], ".", "")
	}
	if mm > 59 || h > 24 {
		return 0, 0, false, false
	}
	ambiguous = ap == "" && word == "" && h >= 1 && h < 12
	switch {
	case ap == "am":
		if h == 12 {
			h = 0
		}
	case ap == "pm":
		if h < 12 {
			h += 12
		}
	case h > 12:
		// 24-hour clock
	case h == 12:
		if word == "midnight" || word == "night" {
			h = 0
		}
	case word == "morning":
	case word == "night" && h <= 5:
	case word == "afternoon" || word == "evening" || word == "night" || word == "noon":
		h += 12
	case h >= 1 && h <= 6:
		h += 12
	}
	if h == 24 {
		h = 0
	}
	return h, mm, true, ambiguous
}

// DescribeWhen words a reminder time for the owner: "today at 12:00 pm", "tomorrow at …", or "Mon 2 Jan at …".
func DescribeWhen(now, at time.Time) string {
	now, at = now.In(ISTZone), at.In(ISTZone)
	clock := at.Format("3:04 pm")
	ny, nm, nd := now.Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, ISTZone)
	day := time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, ISTZone)
	switch {
	case day.Equal(today):
		return "today at " + clock
	case day.Equal(today.AddDate(0, 0, 1)) && now.Hour() >= OwnerDayStartsAt:
		return "tomorrow at " + clock
	}
	return at.Format("Mon 2 Jan") + " at " + clock
}

var (
	reRemindTrigger = regexp.MustCompile(`(?i)\b(?:remind|notify|ping|alert|buzz)\s+me\b|\b(?:set|send|give|put|make|create|schedule)\s+(?:me\s+)?(?:a\s+|an\s+)?(?:reminder|notification|alert|ping)(?:\s+(?:to|for)\s+me)?\b|^\s*reminder\s*:`)
	reQuestionLead  = regexp.MustCompile(`(?i)^\s*(?:how|what|why|when|where|which|who|does|do|is|are)\b`)
	reLeadFiller    = regexp.MustCompile(`(?i)^(?:(?:to|that|about|for|and|me|,|:)\s*\b)*(?:(?:i|we)\s+(?:really\s+|just\s+)?(?:need|have|has|got|must|should|ought|want)\s+to\s+)?`)
	reTrailFiller   = regexp.MustCompile(`(?i)(?:\s+(?:at|on|by|in|for|around|please))*[\s.,;:!?]*$`)
	reSpaces        = regexp.MustCompile(`\s+`)
	stripPatterns   = []*regexp.Regexp{reRelHalf, reRelAn, reRelN, reDayAfter, reTomorrow, reToday, reWeekday, reDateDMon, reDateMonD, reDateSlash, reClockAt, reClockAP, reClock24, rePartWord}
)

// ParseReminder reports whether the message asks for a reminder or a notification, and returns what to be
// reminded of in the owner's words: the time phrase and lead-ins like "that I need to" are removed.
func ParseReminder(text string) (string, bool) {
	if reQuestionLead.MatchString(text) {
		return "", false
	}
	all := reRemindTrigger.FindAllStringIndex(text, -1)
	if all == nil {
		return "", false
	}
	// "Give me a notification to remind me that ...": what to be reminded of follows the last trigger.
	rest := text[all[len(all)-1][1]:]
	for _, re := range stripPatterns {
		rest = re.ReplaceAllStringFunc(rest, func(m string) string { return " " })
	}
	rest = strings.TrimSpace(reSpaces.ReplaceAllString(rest, " "))
	rest = strings.TrimSpace(rest[len(reLeadFiller.FindString(rest)):])
	rest = strings.TrimSpace(rest[:len(rest)-len(reTrailFiller.FindString(rest))])
	if rest == "" {
		return "", false
	}
	return rest, true
}
