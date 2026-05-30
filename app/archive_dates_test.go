package main

import (
	"testing"
	"time"
)

func testAccount(t *testing.T) *Account {
	t.Helper()
	location, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	return &Account{
		SlackUserId:      "U123",
		TimezoneName:     "America/Los_Angeles",
		TimezoneLocation: location,
	}
}

func TestArchiveWindowDefaultUsesPreviousLocalDay(t *testing.T) {
	account := testAccount(t)
	now := time.Date(2026, 5, 23, 14, 30, 0, 0, account.TimezoneLocation)
	window, err := archiveWindowAt(account, false, "", now)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := window.DateString, "2026-05-22"; got != want {
		t.Fatalf("DateString = %q, want %q", got, want)
	}
	if got, want := window.StartTime.Format(time.RFC3339), "2026-05-22T00:00:00-07:00"; got != want {
		t.Fatalf("StartTime = %q, want %q", got, want)
	}
	if got, want := window.EndTime.Format(time.RFC3339), "2026-05-22T23:59:59-07:00"; got != want {
		t.Fatalf("EndTime = %q, want %q", got, want)
	}
}

func TestArchiveWindowExplicitDate(t *testing.T) {
	account := testAccount(t)
	now := time.Date(2026, 5, 23, 14, 30, 0, 0, account.TimezoneLocation)
	window, err := archiveWindowAt(account, false, "2026-05-17", now)
	if err != nil {
		t.Fatal(err)
	}
	if !window.ExplicitDate {
		t.Fatal("ExplicitDate = false, want true")
	}
	if got, want := window.StartTime.Format(time.RFC3339), "2026-05-17T00:00:00-07:00"; got != want {
		t.Fatalf("StartTime = %q, want %q", got, want)
	}
	if got, want := window.EndTime.Format(time.RFC3339), "2026-05-17T23:59:59-07:00"; got != want {
		t.Fatalf("EndTime = %q, want %q", got, want)
	}
}

func TestArchiveWindowRejectsInvalidAndNonHistoricalDates(t *testing.T) {
	account := testAccount(t)
	now := time.Date(2026, 5, 23, 14, 30, 0, 0, account.TimezoneLocation)
	for _, date := range []string{"05/17/2026", "2026-05-23", "2026-05-24"} {
		if _, err := archiveWindowAt(account, false, date, now); err == nil {
			t.Fatalf("archiveWindowAt(%q) succeeded, want error", date)
		}
	}
}

func TestStableArchiveIdempotencyKey(t *testing.T) {
	account := testAccount(t)
	start := time.Date(2026, 5, 17, 0, 0, 0, 0, account.TimezoneLocation)
	got := stableArchiveIdempotencyKey(account, "channel", "C123", "user@example.com", start)
	want := "archive:U123:channel:C123:user@example.com:2026-05-17"
	if got != want {
		t.Fatalf("stableArchiveIdempotencyKey = %q, want %q", got, want)
	}
}
