package sb8200

import (
	"fmt"
	"strings"
	"time"
)

// Status is the connection status page.
type Status struct {
	// Startup is the startup procedure table: each step with its status
	// and the comment beside it, as the page words them.
	Startup []Step
	// Downstream and Upstream are the bonded channels, in the page's
	// order.
	Downstream []Downstream
	Upstream   []Upstream
	// SystemTime is the modem's clock as the page prints it, in UTC for
	// want of a zone on the page.
	SystemTime time.Time
}

// Step is one row of the startup procedure.
type Step struct {
	Procedure string
	Status    string
	Comment   string
}

// Downstream is one downstream bonded channel.
type Downstream struct {
	ID int
	// Locked is the lock status; the page says "Locked" or not.
	Locked bool
	// Modulation as the page words it: "QAM256", or "Other" for the
	// OFDM channel.
	Modulation string
	// Frequency in Hz, Power in dBmV, SNR the signal-to-noise or
	// modulation error ratio in dB.
	Frequency int64
	Power     float64
	SNR       float64
	// Corrected and Uncorrectable codewords since the channel locked.
	Corrected     uint64
	Uncorrectable uint64
}

// Upstream is one upstream bonded channel.
type Upstream struct {
	// Channel is the row's ordinal, ID the channel's.
	Channel int
	ID      int
	Locked  bool
	// Type as the page words it: "SC-QAM Upstream" or "OFDM Upstream".
	Type string
	// Frequency and Width in Hz, Power in dBmV.
	Frequency int64
	Width     int64
	Power     float64
}

// The tables' titles and shapes on the page.
const (
	startupTitle    = "Startup Procedure"
	downstreamTitle = "Downstream Bonded Channels"
	upstreamTitle   = "Upstream Bonded Channels"
	systemTimeLabel = "Current System Time:"
	systemTimeForm  = time.ANSIC

	// Each table opens with a title row and a header row.
	channelHeaderRows = 2
	stepCells         = 3
	downstreamCells   = 8
	upstreamCells     = 7
	lockedWord        = "Locked"
)

func parseStatus(page string) (*Status, error) {
	found, err := tables(page)
	if err != nil {
		return nil, err
	}

	status := &Status{}

	if status.Startup, err = parseStartup(found); err != nil {
		return nil, err
	}

	if status.Downstream, err = parseDownstream(found); err != nil {
		return nil, err
	}

	if status.Upstream, err = parseUpstream(found); err != nil {
		return nil, err
	}

	if status.SystemTime, err = parseSystemTime(page); err != nil {
		return nil, err
	}

	return status, nil
}

func parseStartup(found []table) ([]Step, error) {
	rows, err := titled(found, startupTitle)
	if err != nil {
		return nil, err
	}

	rows, err = body(rows, channelHeaderRows, "startup")
	if err != nil {
		return nil, err
	}

	var steps []Step

	for _, row := range rows {
		if len(row) != stepCells {
			return nil, fmt.Errorf("%w: a startup row of %d cells", ErrPage, len(row))
		}

		steps = append(steps, Step{Procedure: row[0], Status: row[1], Comment: row[2]})
	}

	return steps, nil
}

func parseDownstream(found []table) ([]Downstream, error) {
	rows, err := titled(found, downstreamTitle)
	if err != nil {
		return nil, err
	}

	rows, err = body(rows, channelHeaderRows, "downstream")
	if err != nil {
		return nil, err
	}

	var channels []Downstream

	for _, row := range rows {
		if len(row) != downstreamCells {
			return nil, fmt.Errorf("%w: a downstream row of %d cells", ErrPage, len(row))
		}

		channel, err := downstreamRow(row)
		if err != nil {
			return nil, err
		}

		channels = append(channels, channel)
	}

	return channels, nil
}

func downstreamRow(row []string) (Downstream, error) {
	var (
		channel = Downstream{Locked: row[1] == lockedWord, Modulation: row[2]}
		err     error
	)

	if channel.ID, err = number(row[0]); err != nil {
		return channel, err
	}

	if channel.Frequency, err = hertz(row[3]); err != nil {
		return channel, err
	}

	if channel.Power, err = quantity(row[4], "dBmV"); err != nil {
		return channel, err
	}

	if channel.SNR, err = quantity(row[5], "dB"); err != nil {
		return channel, err
	}

	if channel.Corrected, err = count(row[6]); err != nil {
		return channel, err
	}

	if channel.Uncorrectable, err = count(row[7]); err != nil {
		return channel, err
	}

	return channel, nil
}

func parseUpstream(found []table) ([]Upstream, error) {
	rows, err := titled(found, upstreamTitle)
	if err != nil {
		return nil, err
	}

	rows, err = body(rows, channelHeaderRows, "upstream")
	if err != nil {
		return nil, err
	}

	var channels []Upstream

	for _, row := range rows {
		if len(row) != upstreamCells {
			return nil, fmt.Errorf("%w: an upstream row of %d cells", ErrPage, len(row))
		}

		channel, err := upstreamRow(row)
		if err != nil {
			return nil, err
		}

		channels = append(channels, channel)
	}

	return channels, nil
}

func upstreamRow(row []string) (Upstream, error) {
	var (
		channel = Upstream{Locked: row[2] == lockedWord, Type: row[3]}
		err     error
	)

	if channel.Channel, err = number(row[0]); err != nil {
		return channel, err
	}

	if channel.ID, err = number(row[1]); err != nil {
		return channel, err
	}

	if channel.Frequency, err = hertz(row[4]); err != nil {
		return channel, err
	}

	if channel.Width, err = hertz(row[5]); err != nil {
		return channel, err
	}

	if channel.Power, err = quantity(row[6], "dBmV"); err != nil {
		return channel, err
	}

	return channel, nil
}

// parseSystemTime reads the clock the page prints under the tables, in
// a paragraph rather than a cell.
func parseSystemTime(page string) (time.Time, error) {
	_, after, ok := strings.Cut(page, systemTimeLabel)
	if !ok {
		return time.Time{}, fmt.Errorf("%w: no %q", ErrPage, systemTimeLabel)
	}

	// The label is bold; the clock follows its closing tag.
	after = strings.TrimPrefix(after, "</strong>")
	raw, _, _ := strings.Cut(after, "<")

	return clock(systemTimeForm, strings.TrimSpace(raw))
}
