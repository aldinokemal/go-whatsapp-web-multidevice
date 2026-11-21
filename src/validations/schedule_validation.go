package validations

import (
	"context"
	"strings"
	"time"
	// Windows release binaries have no system zoneinfo to load timezones from.
	_ "time/tzdata"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	validation "github.com/go-ozzo/ozzo-validation/v4"
)

type ScheduleSpec struct {
	ScheduledAt     time.Time
	Timezone        string
	Location        *time.Location
	Recurrence      string
	Weekdays        []int
	DayOfMonth      int
	EndAt           *time.Time
	OccurrenceLimit int
}

func ParseScheduleOptions(options domainSend.ScheduleOptions, now time.Time) (ScheduleSpec, error) {
	if strings.TrimSpace(options.ScheduledAt) == "" {
		return ScheduleSpec{}, nil
	}
	zone := strings.TrimSpace(options.Timezone)
	if zone == "" {
		return ScheduleSpec{}, pkgError.ValidationError("timezone is required for scheduled sends")
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return ScheduleSpec{}, pkgError.ValidationError("timezone must be a valid IANA timezone")
	}
	scheduledAt, err := time.Parse(time.RFC3339, strings.TrimSpace(options.ScheduledAt))
	if err != nil {
		return ScheduleSpec{}, pkgError.ValidationError("scheduled_at must be an RFC3339 timestamp")
	}
	if !scheduledAt.After(now) {
		return ScheduleSpec{}, pkgError.ValidationError("scheduled_at must be in the future")
	}
	recurrence := strings.ToLower(strings.TrimSpace(options.Recurrence))
	if recurrence == "" {
		recurrence = "once"
	}
	if recurrence != "once" && recurrence != "daily" && recurrence != "weekly" && recurrence != "monthly" {
		return ScheduleSpec{}, pkgError.ValidationError("recurrence must be once, daily, weekly, or monthly")
	}
	if recurrence == "once" && (len(options.Weekdays) > 0 || options.DayOfMonth != 0) {
		return ScheduleSpec{}, pkgError.ValidationError("weekly and monthly recurrence fields require a recurring schedule")
	}
	weekdays := append([]int(nil), options.Weekdays...)
	if recurrence == "weekly" {
		if len(weekdays) == 0 {
			return ScheduleSpec{}, pkgError.ValidationError("weekdays are required for weekly recurrence")
		}
		seen := make(map[int]bool, len(weekdays))
		for _, day := range weekdays {
			if day < 0 || day > 6 || seen[day] {
				return ScheduleSpec{}, pkgError.ValidationError("weekdays must contain unique values from 0 to 6")
			}
			seen[day] = true
		}
	}
	if recurrence == "monthly" && (options.DayOfMonth < 1 || options.DayOfMonth > 31) {
		return ScheduleSpec{}, pkgError.ValidationError("day_of_month must be between 1 and 31 for monthly recurrence")
	}
	if recurrence != "monthly" && options.DayOfMonth != 0 {
		return ScheduleSpec{}, pkgError.ValidationError("day_of_month requires monthly recurrence")
	}
	var endAt *time.Time
	if strings.TrimSpace(options.EndAt) != "" {
		parsed, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(options.EndAt))
		if parseErr != nil {
			return ScheduleSpec{}, pkgError.ValidationError("end_at must be an RFC3339 timestamp")
		}
		if !parsed.After(scheduledAt) {
			return ScheduleSpec{}, pkgError.ValidationError("end_at must be after scheduled_at")
		}
		endAt = &parsed
	}
	if options.OccurrenceLimit < 0 {
		return ScheduleSpec{}, pkgError.ValidationError("occurrence_limit cannot be negative")
	}
	if recurrence == "once" && options.OccurrenceLimit > 1 {
		return ScheduleSpec{}, pkgError.ValidationError("occurrence_limit must be 1 or omitted for one-time sends")
	}
	return ScheduleSpec{
		ScheduledAt:     scheduledAt,
		Timezone:        zone,
		Location:        location,
		Recurrence:      recurrence,
		Weekdays:        weekdays,
		DayOfMonth:      options.DayOfMonth,
		EndAt:           endAt,
		OccurrenceLimit: options.OccurrenceLimit,
	}, nil
}

func NextScheduleOccurrence(spec ScheduleSpec, after time.Time) (time.Time, bool) {
	if spec.Recurrence == "once" {
		return time.Time{}, false
	}
	local := after.In(spec.Location)
	startLocal := spec.ScheduledAt.In(spec.Location)
	var candidate time.Time
	switch spec.Recurrence {
	case "daily":
		for offset := 0; offset <= 2; offset++ {
			candidate = occurrenceOn(local.Year(), local.Month(), local.Day()+offset, startLocal)
			if candidate.After(after) {
				break
			}
			candidate = time.Time{}
		}
	case "weekly":
		for offset := 0; offset <= 7; offset++ {
			day := time.Date(local.Year(), local.Month(), local.Day()+offset, 12, 0, 0, 0, spec.Location)
			for _, weekday := range spec.Weekdays {
				if int(day.Weekday()) != weekday {
					continue
				}
				candidate = occurrenceOn(day.Year(), day.Month(), day.Day(), startLocal)
				if candidate.After(after) {
					break
				}
				candidate = time.Time{}
			}
			if !candidate.IsZero() {
				break
			}
		}
	case "monthly":
		for offset := 0; offset <= 24; offset++ {
			month := time.Date(local.Year(), local.Month()+time.Month(offset), 1, 12, 0, 0, 0, spec.Location)
			lastDay := month.AddDate(0, 1, -1).Day()
			day := spec.DayOfMonth
			if day > lastDay {
				day = lastDay
			}
			candidate = occurrenceOn(month.Year(), month.Month(), day, startLocal)
			if candidate.After(after) {
				break
			}
			candidate = time.Time{}
		}
	}
	if candidate.IsZero() || (spec.EndAt != nil && candidate.After(*spec.EndAt)) {
		return time.Time{}, false
	}
	return candidate.UTC(), true
}

// occurrenceOn puts start's wall clock on the given calendar day. A wall time
// inside a DST gap does not exist, and time.Date may resolve it earlier: New
// York's skipped 02:30 becomes 01:30, Santiago's skipped 00:15 lands on the
// previous day. Such a result is pushed forward past the gap, as Go already
// resolves it in zones like Europe/Berlin, so every local day gets at most one
// send and never before its wall time.
func occurrenceOn(year int, month time.Month, day int, start time.Time) time.Time {
	want := time.Date(year, month, day, start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), time.UTC)
	candidate := time.Date(year, month, day, start.Hour(), start.Minute(), start.Second(), start.Nanosecond(), start.Location())
	for wallClock(candidate).Before(want) {
		candidate = candidate.Add(time.Hour)
	}
	return candidate
}

// wallClock reads t's local date and time as if it were UTC, so wall times
// compare without their offsets.
func wallClock(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC)
}

// ValidateListSchedules applies the list page defaults and bounds, mirroring
// ValidateListChats.
func ValidateListSchedules(ctx context.Context, request *domainSend.ScheduleFilter) error {
	if request.Limit == 0 {
		request.Limit = 25
	}

	err := validation.ValidateStructWithContext(ctx, request,
		validation.Field(&request.Limit, validation.Min(1), validation.Max(100)),
		validation.Field(&request.Offset, validation.Min(0)),
		validation.Field(&request.Status, validation.In("active", "running", "paused", "completed", "failed", "cancelled")),
		validation.Field(&request.MessageType, validation.In("text", "image", "file", "video", "audio", "sticker", "contact", "link", "location", "poll", "forward")),
	)

	if err != nil {
		return pkgError.ValidationError(err.Error())
	}

	return nil
}
