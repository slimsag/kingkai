package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	vegeta "github.com/tsenart/vegeta/lib"
)

// attackNameAndMetrics gets the attack name (from the first result) and the
// metrics for the given vegeta gob/.bin file.
func attackNameAndMetrics(filepath string) (string, *vegeta.Metrics, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	// Read each result and add it to the metrics until EOF.
	var (
		d          = vegeta.NewDecoder(f)
		attackName string
		metrics    vegeta.Metrics
	)
	defer metrics.Close()
	for {
		var result vegeta.Result
		err = d.Decode(&result)
		if attackName == "" && result.Attack != "" {
			attackName = result.Attack
		}
		if err == io.EOF {
			break
		} else if err != nil {
			return "", nil, err
		}
		metrics.Add(&result)
	}
	return attackName, &metrics, nil
}

// percentageIncrease calculates the percentage increase between two numbers.
func percentageIncrease(before, after float32) float32 {
	if before == 0 {
		return 0
	}
	return ((after - before) / before) * 100.0
}

type benchmark struct {
	name          string
	before, after *vegeta.Metrics
}

var (
	flagCSV = flag.Bool("csv", false, "output comma seperated values (csv)")
)

func main() {
	// Flag parsing.
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Printf("Usage: %s [-csv] before/ after/\n", os.Args[0])
		os.Exit(1)
	}
	beforePath := flag.Arg(0)
	afterPath := flag.Arg(1)

	// Determine filenames (we just blindly assume after/ filenames match
	// before/ filenames).
	beforeDir, err := os.Open(beforePath)
	if err != nil {
		log.Fatal(err)
	}
	beforeFileInfos, err := beforeDir.Readdir(-1)
	if err != nil {
		log.Fatal(err)
	}
	afterDir, err := os.Open(afterPath)
	if err != nil {
		log.Fatal(err)
	}
	afterFileInfos, err := afterDir.Readdir(-1)
	if err != nil {
		log.Fatal(err)
	}
	var commonFiles []string
	for _, before := range beforeFileInfos {
		for _, after := range afterFileInfos {
			if !before.IsDir() && !after.IsDir() && before.Name() == after.Name() {
				commonFiles = append(commonFiles, before.Name())
				break
			}
		}
	}

	// Read, decode, and sort the metrics.
	var benchmarks []benchmark
	for _, file := range commonFiles {
		name, before, err := attackNameAndMetrics(filepath.Join(beforePath, file))
		if err != nil {
			log.Fatal(err)
		}
		_, after, err := attackNameAndMetrics(filepath.Join(afterPath, file))
		if err != nil {
			log.Fatal(err)
		}
		benchmarks = append(benchmarks, benchmark{name, before, after})
	}
	sort.Slice(benchmarks, func(i, j int) bool {
		return benchmarks[i].name < benchmarks[j].name
	})

	if *flagCSV {
		writeCSV(benchmarks)
	} else {
		writeMarkdown(benchmarks)
	}
}

// smartFormat formats a duration as follows:
//
// 	30.918273ms -> 31ms
// 	30.918273645s -> 30.9s
// 	1m30.918273645s -> 91s
// 	38m30.918273645s -> 2311s
//
func smartFormat(d time.Duration) string {
	if d < time.Second {
		// 30.918273ms -> 31ms
		return d.Round(1 * time.Millisecond).String()
	}
	if d < time.Minute {
		// 30.918273645s -> 30.9s
		return d.Round(100 * time.Millisecond).String()
	}
	// 1m30.918273645s -> 91s
	return fmt.Sprintf("%.0fs", d.Seconds())
}

func writeCSV(benchmarks []benchmark) {
	w := csv.NewWriter(os.Stdout)
	defer w.Flush()

	formatPercentageIncrease := func(before, after time.Duration) string {
		return fmt.Sprintf("%.0f%%", percentageIncrease(float32(before), float32(after)))
	}

	w.Write([]string{
		"Name",
		"Queries per second",
		"Duration",
		"Mean change",
		"P50 change",
		"P95 change",
		"P99 change",
		"Max change",
		"Success before",
		"Success after",
		"",
		"Mean before",
		"Mean after",
		"P50 before",
		"P50 after",
		"P95 before",
		"P95 after",
		"P99 before",
		"P99 after",
		"Max before",
		"Max after",
	})
	for _, b := range benchmarks {
		before := b.before.Latencies
		after := b.after.Latencies
		w.Write([]string{
			b.name,
			fmt.Sprintf("%.0f", b.after.Rate),
			b.after.Duration.Round(3 * time.Second).String(), // 1m58.999964907s -> 2m0s
			formatPercentageIncrease(before.Mean, after.Mean),
			formatPercentageIncrease(before.P50, after.P50),
			formatPercentageIncrease(before.P95, after.P95),
			formatPercentageIncrease(before.P99, after.P99),
			formatPercentageIncrease(before.Max, after.Max),
			fmt.Sprintf("%.1f%%", b.before.Success*100.0),
			fmt.Sprintf("%.1f%%", b.after.Success*100.0),
			"",
			smartFormat(before.Mean),
			smartFormat(after.Mean),
			smartFormat(before.P50),
			smartFormat(after.P50),
			smartFormat(before.P95),
			smartFormat(after.P95),
			smartFormat(before.P99),
			smartFormat(after.P99),
			smartFormat(before.Max),
			smartFormat(after.Max),
		})
	}
}

func writeMarkdown(benchmarks []benchmark) {
	// Go over each file and get and compare the before/after metrics.
	for _, b := range benchmarks {
		// Helper function for formatting the duration difference strings.
		formatDurationDifference := func(before, after time.Duration) string {
			return fmt.Sprintf("%v → %v (%.2f%%)",
				before.Round(time.Millisecond),
				after.Round(time.Millisecond),
				percentageIncrease(float32(before), float32(after)),
			)
		}

		fmt.Println("### " + b.name)
		fmt.Println("")
		fmt.Println("| Mean | P50 | P95 | P99 | Max | Success Ratio |")
		fmt.Println("|------|-----|-----|-----|-----|---------------|")
		fmt.Printf(
			"| %s | %s | %s | %s | %s | %s |\n",
			formatDurationDifference(b.before.Latencies.Mean, b.after.Latencies.Mean),
			formatDurationDifference(b.before.Latencies.P50, b.after.Latencies.P50),
			formatDurationDifference(b.before.Latencies.P95, b.after.Latencies.P95),
			formatDurationDifference(b.before.Latencies.P99, b.after.Latencies.P99),
			formatDurationDifference(b.before.Latencies.Max, b.after.Latencies.Max),
			fmt.Sprintf("%.2f%% → %.2f%%", b.before.Success, b.after.Success),
		)
		fmt.Println("")
	}
}
