package validations

import (
	"fmt"
	"strings"
	"time"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
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
		candidate = time.Date(local.Year(), local.Month(), local.Day(), startLocal.Hour(), startLocal.Minute(), startLocal.Second(), startLocal.Nanosecond(), spec.Location)
		if !candidate.After(after) {
			candidate = candidate.AddDate(0, 0, 1)
		}
	case "weekly":
		for offset := 0; offset <= 7; offset++ {
			day := local.AddDate(0, 0, offset)
			for _, weekday := range spec.Weekdays {
				if int(day.Weekday()) != weekday {
					continue
				}
				candidate = time.Date(day.Year(), day.Month(), day.Day(), startLocal.Hour(), startLocal.Minute(), startLocal.Second(), startLocal.Nanosecond(), spec.Location)
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
			month := time.Date(local.Year(), local.Month(), 1, startLocal.Hour(), startLocal.Minute(), startLocal.Second(), startLocal.Nanosecond(), spec.Location).AddDate(0, offset, 0)
			lastDay := month.AddDate(0, 1, -1).Day()
			day := spec.DayOfMonth
			if day > lastDay {
				day = lastDay
			}
			candidate = time.Date(month.Year(), month.Month(), day, startLocal.Hour(), startLocal.Minute(), startLocal.Second(), startLocal.Nanosecond(), spec.Location)
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

func ScheduleValidationError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("invalid schedule: %w", err)
}
