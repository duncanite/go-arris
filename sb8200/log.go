package sb8200

import (
	"fmt"
	"strings"
	"time"
)

// Event is one row of the event log.
type Event struct {
	// Time by the modem's clock, in UTC for want of a zone on the page,
	// to the minute.
	Time time.Time
	// ID is the DOCSIS event id, Level its priority, 1 the most urgent.
	ID    uint64
	Level int
	// Text is the event's description, the quotes the page wraps it in
	// removed; the modem's and the CMTS's MAC addresses are in it.
	Text string
}

const (
	logTitle     = "Date Time"
	logTimeForm  = "01/02/2006 15:04"
	logHeaderRow = 1
	eventCells   = 4
)

func parseLog(page string) ([]Event, error) {
	found, err := tables(page)
	if err != nil {
		return nil, err
	}

	rows, err := titled(found, logTitle)
	if err != nil {
		return nil, err
	}

	rows, err = body(rows, logHeaderRow, "event")
	if err != nil {
		return nil, err
	}

	var events []Event

	for _, row := range rows {
		if len(row) != eventCells {
			return nil, fmt.Errorf("%w: an event row of %d cells", ErrPage, len(row))
		}

		event := Event{Text: strings.Trim(row[3], `"`)}

		if event.Time, err = clock(logTimeForm, row[0]); err != nil {
			return nil, err
		}

		if event.ID, err = count(row[1]); err != nil {
			return nil, err
		}

		if event.Level, err = number(row[2]); err != nil {
			return nil, err
		}

		events = append(events, event)
	}

	return events, nil
}
