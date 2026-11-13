package sb8200

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

// Write prints the status as a person reads it: the startup procedure,
// the downstream and upstream channels as tables, and the modem's clock.
func (s *Status) Write(out io.Writer) error {
	// A table per section: one tabwriter would align the startup steps'
	// columns with the channels'.
	table := columns(out)

	fmt.Fprintln(table, "Startup")

	for _, step := range s.Startup {
		fmt.Fprintf(table, "  %s\t%s\t%s\n", step.Procedure, step.Status, step.Comment)
	}

	if err := table.Flush(); err != nil {
		return fmt.Errorf("writing the status: %w", err)
	}

	table = columns(out)

	fmt.Fprintf(table, "\nDownstream (%d channels)\n", len(s.Downstream))
	fmt.Fprintln(table, "  ID\tLock\tModulation\tFrequency\tPower\tSNR/MER\tCorrected\tUncorrectable")

	for _, channel := range s.Downstream {
		fmt.Fprintf(table, "  %d\t%s\t%s\t%d Hz\t%.1f dBmV\t%.1f dB\t%d\t%d\n",
			channel.ID, lock(channel.Locked), channel.Modulation, channel.Frequency,
			channel.Power, channel.SNR, channel.Corrected, channel.Uncorrectable)
	}

	if err := table.Flush(); err != nil {
		return fmt.Errorf("writing the status: %w", err)
	}

	table = columns(out)

	fmt.Fprintf(table, "\nUpstream (%d channels)\n", len(s.Upstream))
	fmt.Fprintln(table, "  Channel\tID\tLock\tType\tFrequency\tWidth\tPower")

	for _, channel := range s.Upstream {
		fmt.Fprintf(table, "  %d\t%d\t%s\t%s\t%d Hz\t%d Hz\t%.1f dBmV\n",
			channel.Channel, channel.ID, lock(channel.Locked), channel.Type,
			channel.Frequency, channel.Width, channel.Power)
	}

	if err := table.Flush(); err != nil {
		return fmt.Errorf("writing the status: %w", err)
	}

	_, err := fmt.Fprintf(out, "\nSystem time: %s, by the modem's clock\n", s.SystemTime.Format(time.DateTime))
	if err != nil {
		return fmt.Errorf("writing the status: %w", err)
	}

	return nil
}

// Write prints the product information, one labelled line per value.
func (i *Info) Write(out io.Writer) error {
	table := columns(out)

	fmt.Fprintf(table, "Specification:\t%s\n", i.Specification)
	fmt.Fprintf(table, "Hardware:\t%s\n", i.Hardware)
	fmt.Fprintf(table, "Software:\t%s\n", i.Software)
	fmt.Fprintf(table, "MAC:\t%s\n", i.MAC)
	fmt.Fprintf(table, "Serial:\t%s\n", i.Serial)
	fmt.Fprintf(table, "Uptime:\t%s\n", i.Uptime)

	if err := table.Flush(); err != nil {
		return fmt.Errorf("writing the information: %w", err)
	}

	return nil
}

// WriteLog prints events one per line, in the order given: time, id,
// level, text.
func WriteLog(out io.Writer, events []Event) error {
	table := columns(out)

	for _, event := range events {
		fmt.Fprintf(table, "%s\t%d\t%d\t%s\n",
			event.Time.Format("2006-01-02 15:04"), event.ID, event.Level, event.Text)
	}

	if err := table.Flush(); err != nil {
		return fmt.Errorf("writing the log: %w", err)
	}

	return nil
}

func columns(out io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
}

func lock(locked bool) string {
	if locked {
		return "locked"
	}

	return "not locked"
}
