package sb8200

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

// Info is the product information page.
type Info struct {
	// Specification is the DOCSIS the modem complies with, as the page
	// words it.
	Specification string
	Hardware      string
	Software      string
	// MAC is the cable modem's, the one the cable operator knows it by.
	MAC    string
	Serial string
	Uptime time.Duration
}

// The page's labels, and the shape of its uptime, "3 days 21h:32m:57s.00".
const (
	infoTitle    = "Information"
	uptimeTitle  = "Status"
	labelSpec    = "Standard Specification Compliant"
	labelHW      = "Hardware Version"
	labelSW      = "Software Version"
	labelMAC     = "Cable Modem MAC Address"
	labelSerial  = "Serial Number"
	labelUptime  = "Up Time"
	hoursPerDay  = 24
	uptimeFields = 5
)

var uptimeForm = regexp.MustCompile(`^(?:(\d+) days? )?(\d+)h:(\d+)m:(\d+)s(?:\.\d+)?$`)

func parseInfo(page string) (*Info, error) {
	found, err := tables(page)
	if err != nil {
		return nil, err
	}

	information, err := titled(found, infoTitle)
	if err != nil {
		return nil, err
	}

	status, err := titled(found, uptimeTitle)
	if err != nil {
		return nil, err
	}

	values := labelled(information, status)

	info := &Info{
		Specification: values[labelSpec],
		Hardware:      values[labelHW],
		Software:      values[labelSW],
		MAC:           values[labelMAC],
		Serial:        values[labelSerial],
	}

	for label, value := range map[string]string{
		labelSpec: info.Specification, labelHW: info.Hardware, labelSW: info.Software,
		labelMAC: info.MAC, labelSerial: info.Serial,
	} {
		if value == "" {
			return nil, fmt.Errorf("%w: no %q", ErrPage, label)
		}
	}

	if info.Uptime, err = uptime(values[labelUptime]); err != nil {
		return nil, err
	}

	return info, nil
}

// labelled reads two-cell rows as label and value.
func labelled(found ...table) map[string]string {
	values := map[string]string{}

	for _, rows := range found {
		for _, row := range rows {
			if len(row) == 2 {
				values[row[0]] = row[1]
			}
		}
	}

	return values
}

func uptime(cell string) (time.Duration, error) {
	fields := uptimeForm.FindStringSubmatch(cell)
	if len(fields) != uptimeFields {
		return 0, fmt.Errorf("%w: %q is not an uptime", ErrPage, cell)
	}

	var parts [uptimeFields - 1]int

	for index, field := range fields[1:] {
		if field == "" {
			continue
		}

		value, err := strconv.Atoi(field)
		if err != nil {
			return 0, fmt.Errorf("%w: %q is not an uptime", ErrPage, cell)
		}

		parts[index] = value
	}

	days, hours, minutes, seconds := parts[0], parts[1], parts[2], parts[3]

	return time.Duration(days*hoursPerDay+hours)*time.Hour +
		time.Duration(minutes)*time.Minute +
		time.Duration(seconds)*time.Second, nil
}
