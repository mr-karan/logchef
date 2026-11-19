package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReportScheduleValidate(t *testing.T) {
	n := func(v int) *int { return &v }
	valid := map[string]ReportSchedule{
		"hourly at :00":       {Frequency: ReportHourly},
		"hourly at :59":       {Frequency: ReportHourly, Minute: 59},
		"daily 00:00":         {Frequency: ReportDaily, Hour: n(0)},
		"daily 23:59":         {Frequency: ReportDaily, Hour: n(23), Minute: 59},
		"weekdays 09:00":      {Frequency: ReportWeekly, Hour: n(9), Weekdays: []int{1, 2, 3, 4, 5}},
		"sunday and saturday": {Frequency: ReportWeekly, Hour: n(9), Weekdays: []int{0, 6}},
		"monthly on the 1st":  {Frequency: ReportMonthly, Hour: n(9), DayOfMonth: n(1)},
		"monthly on the 28th": {Frequency: ReportMonthly, Hour: n(9), DayOfMonth: n(28)},
	}
	for name, s := range valid {
		if err := s.Validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	invalid := []struct {
		name  string
		s     ReportSchedule
		field string
	}{
		{"no frequency", ReportSchedule{Hour: n(9)}, "schedule.frequency"},
		{"unknown frequency", ReportSchedule{Frequency: "minutely"}, "schedule.frequency"},
		{"negative minute", ReportSchedule{Frequency: ReportHourly, Minute: -1}, "schedule.minute"},
		{"minute 60", ReportSchedule{Frequency: ReportDaily, Hour: n(9), Minute: 60}, "schedule.minute"},
		{"hourly with an hour", ReportSchedule{Frequency: ReportHourly, Hour: n(9)}, "schedule.hour"},
		{"hourly with weekdays", ReportSchedule{Frequency: ReportHourly, Weekdays: []int{1}}, "schedule.weekdays"},
		{"hourly with a day of month", ReportSchedule{Frequency: ReportHourly, DayOfMonth: n(1)}, "schedule.day_of_month"},
		{"daily without an hour", ReportSchedule{Frequency: ReportDaily}, "schedule.hour"},
		{"daily hour 24", ReportSchedule{Frequency: ReportDaily, Hour: n(24)}, "schedule.hour"},
		{"daily negative hour", ReportSchedule{Frequency: ReportDaily, Hour: n(-1)}, "schedule.hour"},
		{"daily with weekdays", ReportSchedule{Frequency: ReportDaily, Hour: n(9), Weekdays: []int{1}}, "schedule.weekdays"},
		{"daily with an empty weekdays list", ReportSchedule{Frequency: ReportDaily, Hour: n(9), Weekdays: []int{}}, "schedule.weekdays"},
		{"daily with a day of month", ReportSchedule{Frequency: ReportDaily, Hour: n(9), DayOfMonth: n(1)}, "schedule.day_of_month"},
		{"weekly without an hour", ReportSchedule{Frequency: ReportWeekly, Weekdays: []int{1}}, "schedule.hour"},
		{"weekly without weekdays", ReportSchedule{Frequency: ReportWeekly, Hour: n(9)}, "schedule.weekdays"},
		{"weekly with an empty weekdays list", ReportSchedule{Frequency: ReportWeekly, Hour: n(9), Weekdays: []int{}}, "schedule.weekdays"},
		{"weekday 7", ReportSchedule{Frequency: ReportWeekly, Hour: n(9), Weekdays: []int{7}}, "schedule.weekdays"},
		{"negative weekday", ReportSchedule{Frequency: ReportWeekly, Hour: n(9), Weekdays: []int{-1}}, "schedule.weekdays"},
		{"repeated weekday", ReportSchedule{Frequency: ReportWeekly, Hour: n(9), Weekdays: []int{1, 1}}, "schedule.weekdays"},
		{"weekly with a day of month", ReportSchedule{Frequency: ReportWeekly, Hour: n(9), Weekdays: []int{1}, DayOfMonth: n(1)}, "schedule.day_of_month"},
		{"monthly without an hour", ReportSchedule{Frequency: ReportMonthly, DayOfMonth: n(1)}, "schedule.hour"},
		{"monthly without a day", ReportSchedule{Frequency: ReportMonthly, Hour: n(9)}, "schedule.day_of_month"},
		{"day of month 0", ReportSchedule{Frequency: ReportMonthly, Hour: n(9), DayOfMonth: n(0)}, "schedule.day_of_month"},
		{"day of month 29", ReportSchedule{Frequency: ReportMonthly, Hour: n(9), DayOfMonth: n(29)}, "schedule.day_of_month"},
		{"monthly with weekdays", ReportSchedule{Frequency: ReportMonthly, Hour: n(9), DayOfMonth: n(1), Weekdays: []int{1}}, "schedule.weekdays"},
	}
	for _, tc := range invalid {
		err := tc.s.Validate()
		if err == nil {
			t.Errorf("%s: Validate passed", tc.name)
			continue
		}
		if !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s: error %q does not name %s", tc.name, err, tc.field)
		}
	}
}

func TestParseReportSchedule(t *testing.T) {
	s, err := ParseReportSchedule([]byte(`{"frequency":"weekly","minute":30,"hour":8,"weekdays":[1,5]}`))
	if err != nil {
		t.Fatal(err)
	}
	if s.Frequency != ReportWeekly || s.Minute != 30 || *s.Hour != 8 || len(s.Weekdays) != 2 {
		t.Errorf("parsed %+v", s)
	}
	out, err := json.Marshal(ReportSchedule{Frequency: ReportHourly, Minute: 5})
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"frequency":"hourly","minute":5}` {
		t.Errorf("an hourly schedule encodes as %s, want the unused fields absent", out)
	}

	for raw, want := range map[string]string{
		`{"frequency":"daily","hour":9,"minute":0,"seconds":10}`: "unknown field",
		`{"frequency":"daily","hour":9}{}`:                       "trailing data",
		`{"frequency":"daily","hour":"9"}`:                       "cannot unmarshal",
		`{"frequency":"hourly","hour":9}`:                        "schedule.hour",
		`{"frequency":"daily","hour":null}`:                      "schedule.hour",
	} {
		if _, err := ParseReportSchedule([]byte(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseReportSchedule(%s) = %v, want an error with %q", raw, err, want)
		}
	}
}
