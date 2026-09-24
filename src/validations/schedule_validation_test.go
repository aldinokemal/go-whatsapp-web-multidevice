package validations

import (
	"context"
	"testing"
	"time"

	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	"github.com/stretchr/testify/require"
)

func TestParseScheduleOptionsValidatesRecurrence(t *testing.T) {
	now := time.Date(2026, 9, 21, 10, 0, 0, 0, time.UTC)
	base := domainSend.ScheduleOptions{
		ScheduledAt: now.Add(time.Hour).Format(time.RFC3339),
		Timezone:    "Asia/Jakarta",
	}

	tests := []struct {
		name    string
		options domainSend.ScheduleOptions
		wantErr string
	}{
		{name: "valid weekly", options: func() domainSend.ScheduleOptions {
			o := base
			o.Recurrence = "weekly"
			o.Weekdays = []int{1, 3}
			return o
		}()},
		{name: "invalid timezone", options: func() domainSend.ScheduleOptions { o := base; o.Timezone = "Mars/Olympus"; return o }(), wantErr: "timezone"},
		{name: "weekly needs weekday", options: func() domainSend.ScheduleOptions { o := base; o.Recurrence = "weekly"; return o }(), wantErr: "weekdays"},
		{name: "monthly needs day", options: func() domainSend.ScheduleOptions { o := base; o.Recurrence = "monthly"; return o }(), wantErr: "day_of_month"},
		{name: "end must be later", options: func() domainSend.ScheduleOptions {
			o := base
			o.EndAt = now.Add(30 * time.Minute).Format(time.RFC3339)
			return o
		}(), wantErr: "end_at"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseScheduleOptions(tt.options, now)
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestNextScheduleOccurrenceClampsShortMonths(t *testing.T) {
	location, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	spec := ScheduleSpec{
		ScheduledAt: time.Date(2026, 1, 31, 9, 0, 0, 0, location),
		Location:    location,
		Recurrence:  "monthly",
		DayOfMonth:  31,
	}
	next, ok := NextScheduleOccurrence(spec, time.Date(2026, 1, 31, 10, 0, 0, 0, location))
	require.True(t, ok)
	assertDay := next.In(location)
	require.Equal(t, 28, assertDay.Day())
	require.Equal(t, time.February, assertDay.Month())
}

// Consecutive worker runs feed each send time back in as "after"; across a DST
// transition every local day must still get exactly one send.
func TestNextScheduleOccurrenceDailyAcrossDST(t *testing.T) {
	tests := []struct {
		name  string
		zone  string
		start string
	}{
		{name: "santiago gap skips midnight", zone: "America/Santiago", start: "2027-09-02T00:15:00"},
		{name: "new york spring gap", zone: "America/New_York", start: "2027-03-11T02:30:00"},
		{name: "new york autumn overlap", zone: "America/New_York", start: "2027-11-05T01:30:00"},
		{name: "berlin spring gap", zone: "Europe/Berlin", start: "2027-03-25T02:30:00"},
		{name: "berlin autumn overlap", zone: "Europe/Berlin", start: "2027-10-28T02:30:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			location, err := time.LoadLocation(tt.zone)
			require.NoError(t, err)
			start, err := time.ParseInLocation("2006-01-02T15:04:05", tt.start, location)
			require.NoError(t, err)
			spec := ScheduleSpec{ScheduledAt: start, Location: location, Recurrence: "daily"}

			prev := start
			// Count calendar days in UTC: AddDate on the local start would itself
			// fall into the Santiago gap.
			wantDay := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
			for i := 0; i < 6; i++ {
				next, ok := NextScheduleOccurrence(spec, prev)
				require.True(t, ok)
				require.True(t, next.After(prev), "run %d: %s is not after %s", i, next, prev)
				wantDay = wantDay.AddDate(0, 0, 1)
				local := next.In(location)
				require.Equal(t, wantDay.Format(time.DateOnly), local.Format(time.DateOnly), "run %d", i)
				prev = next
			}
		})
	}
}

func TestNextScheduleOccurrenceSantiagoGapLandsOnIntendedDay(t *testing.T) {
	location, err := time.LoadLocation("America/Santiago")
	require.NoError(t, err)
	spec := ScheduleSpec{ScheduledAt: time.Date(2027, 9, 1, 0, 15, 0, 0, location), Location: location, Recurrence: "daily"}

	next, ok := NextScheduleOccurrence(spec, time.Date(2027, 9, 4, 0, 15, 0, 0, location))
	require.True(t, ok)
	require.Equal(t, "2027-09-05 01:15", next.In(location).Format("2006-01-02 15:04"))
}

func TestNextScheduleOccurrenceGapTimeMovesForward(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)
	spec := ScheduleSpec{ScheduledAt: time.Date(2027, 3, 11, 2, 30, 0, 0, location), Location: location, Recurrence: "daily"}

	// 02:30 does not exist on 2027-03-14; Go alone would answer 01:30.
	next, ok := NextScheduleOccurrence(spec, time.Date(2027, 3, 13, 2, 30, 0, 0, location))
	require.True(t, ok)
	require.Equal(t, "2027-03-14 03:30", next.In(location).Format("2006-01-02 15:04"))
}

func TestNextScheduleOccurrenceWeeklyWraps(t *testing.T) {
	location, err := time.LoadLocation("Asia/Jakarta")
	require.NoError(t, err)
	// 2027-01-04 is a Monday.
	spec := ScheduleSpec{ScheduledAt: time.Date(2027, 1, 4, 9, 0, 0, 0, location), Location: location, Recurrence: "weekly", Weekdays: []int{1}}

	next, ok := NextScheduleOccurrence(spec, time.Date(2027, 1, 4, 9, 0, 0, 0, location))
	require.True(t, ok)
	require.Equal(t, "2027-01-11 09:00", next.In(location).Format("2006-01-02 15:04"))
}

func TestNextScheduleOccurrenceStopsAtEndAt(t *testing.T) {
	location, err := time.LoadLocation("UTC")
	require.NoError(t, err)
	start := time.Date(2027, 1, 1, 9, 0, 0, 0, location)
	endAt := time.Date(2027, 1, 2, 8, 0, 0, 0, location)
	spec := ScheduleSpec{ScheduledAt: start, Location: location, Recurrence: "daily", EndAt: &endAt}

	_, ok := NextScheduleOccurrence(spec, start)
	require.False(t, ok)
}

func TestValidateListSchedules(t *testing.T) {
	tests := []struct {
		name      string
		filter    domainSend.ScheduleFilter
		wantErr   string
		wantLimit int
	}{
		{name: "defaults the limit", filter: domainSend.ScheduleFilter{}, wantLimit: 25},
		{name: "accepts filters", filter: domainSend.ScheduleFilter{Limit: 100, Status: "paused", MessageType: "forward"}, wantLimit: 100},
		{name: "rejects a large limit", filter: domainSend.ScheduleFilter{Limit: 101}, wantErr: "limit"},
		{name: "rejects a negative limit", filter: domainSend.ScheduleFilter{Limit: -1}, wantErr: "limit"},
		{name: "rejects a negative offset", filter: domainSend.ScheduleFilter{Offset: -1}, wantErr: "offset"},
		{name: "rejects an unknown status", filter: domainSend.ScheduleFilter{Status: "done"}, wantErr: "status"},
		{name: "rejects an unknown message type", filter: domainSend.ScheduleFilter{MessageType: "presence"}, wantErr: "message_type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter := tt.filter
			err := ValidateListSchedules(context.Background(), &filter)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantLimit, filter.Limit)
		})
	}
}
