package main

import (
	"errors"
	"time"
)

const ArchiveDateInputFormat = "2006-01-02"

type ArchiveWindow struct {
	StartTime    time.Time
	EndTime      time.Time
	DateString   string
	ExplicitDate bool
}

func archiveWindow(account *Account, devMode bool, dateString string) (ArchiveWindow, error) {
	return archiveWindowAt(account, devMode, dateString, time.Now().In(account.TimezoneLocation))
}

func archiveWindowAt(account *Account, devMode bool, dateString string, now time.Time) (ArchiveWindow, error) {
	if dateString != "" {
		return explicitArchiveWindow(account.TimezoneLocation, dateString, now)
	}
	if devMode {
		start := now.AddDate(0, 0, -1)
		return ArchiveWindow{
			StartTime:  start,
			EndTime:    now,
			DateString: start.In(account.TimezoneLocation).Format(ArchiveDateInputFormat),
		}, nil
	}
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -1)
	end := start.AddDate(0, 0, 1).Add(-time.Second)
	return ArchiveWindow{
		StartTime:  start,
		EndTime:    end,
		DateString: start.Format(ArchiveDateInputFormat),
	}, nil
}

func explicitArchiveWindow(location *time.Location, dateString string, now time.Time) (ArchiveWindow, error) {
	start, err := time.ParseInLocation(ArchiveDateInputFormat, dateString, location)
	if err != nil {
		return ArchiveWindow{}, errors.New("date must use YYYY-MM-DD format")
	}
	today := time.Date(now.In(location).Year(), now.In(location).Month(), now.In(location).Day(), 0, 0, 0, 0, location)
	if !start.Before(today) {
		return ArchiveWindow{}, errors.New("date must be before today")
	}
	return ArchiveWindow{
		StartTime:    start,
		EndTime:      start.AddDate(0, 0, 1).Add(-time.Second),
		DateString:   start.Format(ArchiveDateInputFormat),
		ExplicitDate: true,
	}, nil
}

func defaultArchiveDate(account *Account) string {
	window, err := archiveWindow(account, false, "")
	if err != nil {
		return ""
	}
	return window.DateString
}
