package cbt

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/bigtable"
	bt "github.com/takashabe/btcli/pkg/bigtable"
	"github.com/takashabe/btcli/pkg/printer"
)

func DoLS(ctx context.Context, client bt.Client, args ...string) {
	tables, err := client.Tables(ctx)
	if err != nil {
		fmt.Fprintf(client.ErrStream(), "%v", err)
		return
	}
	for _, tbl := range tables {
		fmt.Fprintln(client.OutStream(), tbl)
	}
}

func DoCount(ctx context.Context, client bt.Client, args ...string) {
	if len(args) < 1 {
		fmt.Fprintln(client.ErrStream(), "Invalid args: count <table>")
		return
	}
	table := args[0]
	cnt, err := client.Count(ctx, table)
	if err != nil {
		fmt.Fprintf(client.ErrStream(), "%v", err)
		return
	}
	fmt.Fprintln(client.OutStream(), cnt)
}

func DoLookup(ctx context.Context, client bt.Client, args ...string) {
	if len(args) < 2 {
		fmt.Fprintln(client.ErrStream(), "Invalid args: lookup <table> <row>")
		return
	}
	table := args[0]
	key := args[1]
	opts := args[2:]

	parsed := make(map[string]string)
	for _, opt := range opts {
		i := strings.Index(opt, "=")
		if i < 0 {
			fmt.Fprintf(client.ErrStream(), "Invalid option: %v\n", opt)
			return
		}
		// TODO: Improve parsing opts
		k, v := opt[:i], opt[i+1:]
		switch k {
		default:
			fmt.Fprintf(client.ErrStream(), "Unknown option: %v\n", opt)
			return
		case "decode", "decode_columns":
			parsed[k] = v
		case "version":
			parsed[k] = v
		}
	}

	ro, err := readOption(parsed)
	if err != nil {
		fmt.Fprintf(client.ErrStream(), "Invalid options: %v\n", err)
		return
	}

	b, err := client.Get(ctx, table, key, ro...)
	if err != nil {
		fmt.Fprintf(client.OutStream(), "%v", err)
		return
	}
	row := b.Rows[0]

	// decode options
	p := &printer.Printer{
		OutStream:        client.OutStream(),
		DecodeType:       decodeGlobalOption(parsed),
		DecodeColumnType: decodeColumnOption(parsed),
	}
	p.PrintRow(row)
}

func DoRead(ctx context.Context, client bt.Client, args ...string) {
	if len(args) < 1 {
		fmt.Fprintln(client.ErrStream(), "Invalid args: read <table> [args ...]")
		return
	}
	table := args[0]
	opts := args[1:]

	parsed := make(map[string]string)
	for _, opt := range opts {
		i := strings.Index(opt, "=")
		if i < 0 {
			fmt.Fprintf(os.Stderr, "Invalid option: %v\n", opt)
			return
		}
		// TODO: Improve parsing opts
		key, val := opt[:i], opt[i+1:]
		switch key {
		default:
			fmt.Fprintf(os.Stderr, "Unknown option: %v\n", opt)
			return
		case "decode", "decode_columns":
			parsed[key] = val
		case "count", "start", "end", "prefix", "version", "family", "value", "from", "to":
			parsed[key] = val
		}
	}

	if (parsed["start"] != "" || parsed["end"] != "") && parsed["prefix"] != "" {
		fmt.Fprintf(client.ErrStream(), `"start"/"end" may not be mixed with "prefix"`)
		return
	}

	rr, err := rowRange(parsed)
	if err != nil {
		fmt.Fprintf(client.ErrStream(), "Invlaid range: %v\n", err)
		return
	}
	ro, err := readOption(parsed)
	if err != nil {
		fmt.Fprintf(client.ErrStream(), "Invalid options: %v\n", err)
		return
	}

	b, err := client.GetRows(ctx, table, rr, ro...)
	if err != nil {
		fmt.Fprintf(client.ErrStream(), "%v\n", err)
		return
	}
	rows := b.Rows

	// decode options
	p := &printer.Printer{
		OutStream:        client.OutStream(),
		DecodeType:       decodeGlobalOption(parsed),
		DecodeColumnType: decodeColumnOption(parsed),
	}
	p.PrintRows(rows)
}

func rowRange(parsedArgs map[string]string) (bigtable.RowRange, error) {
	var rr bigtable.RowRange
	if start, end := parsedArgs["start"], parsedArgs["end"]; end != "" {
		rr = bigtable.NewRange(start, end)
	} else if start != "" {
		rr = bigtable.InfiniteRange(start)
	}
	if prefix := parsedArgs["prefix"]; prefix != "" {
		rr = bigtable.PrefixRange(prefix)
	}

	return rr, nil
}

func readOption(parsedArgs map[string]string) ([]bigtable.ReadOption, error) {
	var (
		opts []bigtable.ReadOption
		fils []bigtable.Filter
	)

	// filters
	if regex := parsedArgs["regex"]; regex != "" {
		fils = append(fils, bigtable.RowKeyFilter(regex))
	}
	if family := parsedArgs["family"]; family != "" {
		fils = append(fils, bigtable.FamilyFilter(fmt.Sprintf("^%s$", family)))
	}
	if version := parsedArgs["version"]; version != "" {
		n, err := strconv.ParseInt(version, 0, 64)
		if err != nil {
			return nil, err
		}
		fils = append(fils, bigtable.LatestNFilter(int(n)))
	}
	var startTime, endTime time.Time
	if from := parsedArgs["from"]; from != "" {
		t, err := strconv.ParseInt(from, 0, 64)
		if err != nil {
			return nil, err
		}
		startTime = time.Unix(t, 0)
	}
	if to := parsedArgs["to"]; to != "" {
		t, err := strconv.ParseInt(to, 0, 64)
		if err != nil {
			return nil, err
		}
		endTime = time.Unix(t, 0)
	}
	if !startTime.IsZero() || !endTime.IsZero() {
		fils = append(fils, bigtable.TimestampRangeFilter(startTime, endTime))
	}
	if value := parsedArgs["value"]; value != "" {
		fils = append(fils, bigtable.ValueFilter(fmt.Sprintf("%s", value)))
	}

	if len(fils) == 1 {
		opts = append(opts, bigtable.RowFilter(fils[0]))
	} else if len(fils) > 1 {
		opts = append(opts, bigtable.RowFilter(bigtable.ChainFilters(fils...)))
	}

	// isolated readOption
	if count := parsedArgs["count"]; count != "" {
		n, err := strconv.ParseInt(count, 0, 64)
		if err != nil {
			return nil, err
		}
		opts = append(opts, bigtable.LimitRows(n))
	}
	return opts, nil
}

func decodeGlobalOption(parsedArgs map[string]string) string {
	if d := parsedArgs["decode"]; d != "" {
		return d
	}
	return os.Getenv("BTCLI_DECODE_TYPE")
}

func decodeColumnOption(parsedArgs map[string]string) map[string]string {
	arg := parsedArgs["decode_columns"]
	if len(arg) == 0 {
		return map[string]string{}
	}

	ds := strings.Split(arg, ",")
	ret := map[string]string{}
	for _, d := range ds {
		ct := strings.SplitN(d, ":", 2)
		if len(ct) != 2 {
			continue
		}
		ret[ct[0]] = ct[1]
	}
	return ret
}
