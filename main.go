package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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

func main() {
	// Flag parsing.
	if len(os.Args) <= 2 {
		fmt.Printf("Usage: %s before/ after/\n", os.Args[0])
		os.Exit(1)
	}
	beforePath := os.Args[1]
	afterPath := os.Args[2]

	// Determine filenames (we just blindly assume after/ filenames match
	// before/ filenames).
	beforeDir, err := os.Open(beforePath)
	if err != nil {
		log.Fatal(err)
	}
	fileInfos, err := beforeDir.Readdir(-1)
	if err != nil {
		log.Fatal(err)
	}

	// Go over each file and get and compare the before/after metrics.
	for _, fi := range fileInfos {
		if fi.IsDir() {
			continue
		}

		// Get the metrics
		attackName, beforeMetrics, err := attackNameAndMetrics(filepath.Join(beforePath, fi.Name()))
		if err != nil {
			log.Fatal(err)
		}
		_, afterMetrics, err := attackNameAndMetrics(filepath.Join(afterPath, fi.Name()))
		if err != nil {
			log.Fatal(err)
		}

		// Helper function for formatting the duration difference strings.
		formatDurationDifference := func(before time.Duration, after time.Duration) string {
			before = before.Round(time.Millisecond)
			after = after.Round(time.Millisecond)
			return fmt.Sprintf("%v → %v (%.2f%%)", before, after, percentageIncrease(float32(before), float32(after)))
		}

		fmt.Println("### " + attackName)
		fmt.Println("")
		fmt.Println("| Mean | P50 | P95 | P99 | Max | Success Ratio |")
		fmt.Println("|------|-----|-----|-----|-----|---------------|")
		fmt.Printf(
			"| %s | %s | %s | %s | %s | %s |\n",
			formatDurationDifference(beforeMetrics.Latencies.Mean, afterMetrics.Latencies.Mean),
			formatDurationDifference(beforeMetrics.Latencies.P50, afterMetrics.Latencies.P50),
			formatDurationDifference(beforeMetrics.Latencies.P95, afterMetrics.Latencies.P95),
			formatDurationDifference(beforeMetrics.Latencies.P99, afterMetrics.Latencies.P99),
			formatDurationDifference(beforeMetrics.Latencies.Max, afterMetrics.Latencies.Max),
			fmt.Sprintf("%v%% → %v%%", beforeMetrics.Success, afterMetrics.Success),
		)
		fmt.Println("")
	}
}
