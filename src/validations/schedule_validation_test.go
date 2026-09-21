package validations

import (
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
