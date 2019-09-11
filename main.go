package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"

	"github.com/360EntSecGroup-Skylar/excelize/v2"
	vegeta "github.com/tsenart/vegeta/lib"
)

// attackNameAndMetrics gets the attack name (from the first result) and the
// metrics for the given vegeta gob/.bin file.
func attackNameAndMetrics(filepath string) (string, *vegeta.Metrics, uint64, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return "", nil, 0, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return "", nil, 0, err
	}
	fileSize := uint64(fi.Size())

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
			return "", nil, 0, err
		}
		metrics.Add(&result)
	}
	return attackName, &metrics, fileSize, nil
}

// percentageIncrease calculates the percentage increase between two numbers.
func percentageIncrease(before, after float64) float64 {
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
	flagCSV  = flag.Bool("csv", false, "output comma seperated values (csv)")
	flagXLSX = flag.Bool("xlsx", false, "output colored Google Sheets/Excel document (xlsx)")

	flagProgress                = flag.Bool("progress", false, "print progress messages to stderr")
	flagTotalRequestsMargin     = flag.Int("total-requests-margin", 0, "margin of error for total requests (in # requests)")
	flagThroughputMargin        = flag.Float64("throughput-margin", 0, "margin of error for throughput (in # requests)")
	flagMeanBytesSentMargin     = flag.Float64("mean-bytes-sent-margin", 0, "margin of error for mean bytes sent (in # bytes)")
	flagMeanBytesReceivedMargin = flag.Float64("mean-bytes-received-margin", 0, "margin of error for mean bytes received (in # bytes)")
	flagSuccessMargin           = flag.Float64("success-margin", 0, "margin of error for success percentage (in percentage of percentage, 0.0 - 100.0)")
	flagRequestDurationMargin   = flag.Duration("request-duration-margin", 0, "margin of error for P50, P95, P99, Max (e.g. \"30ms\")")
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
	var (
		benchmarks                  []benchmark
		datasetTotal, requestsTotal uint64
	)
	for i, file := range commonFiles {
		name, before, fileSize, err := attackNameAndMetrics(filepath.Join(beforePath, file))
		if err != nil {
			log.Fatal(filepath.Join(beforePath, file), err)
		}
		datasetTotal += fileSize
		requestsTotal += before.Requests
		_, after, fileSize, err := attackNameAndMetrics(filepath.Join(afterPath, file))
		if err != nil {
			log.Fatal(filepath.Join(afterPath, file), err)
		}
		datasetTotal += fileSize
		requestsTotal += after.Requests
		if *flagProgress {
			fmt.Fprintln(os.Stderr, "Consumed", datasetTotal, "bytes,", requestsTotal, "requests, from", i+1, "files")
		}
		benchmarks = append(benchmarks, benchmark{name, before, after})
	}
	sort.Slice(benchmarks, func(i, j int) bool {
		return benchmarks[i].name < benchmarks[j].name
	})

	if *flagCSV {
		writeCSV(benchmarks)
	} else if *flagXLSX {
		writeXLSX(benchmarks, requestsTotal, datasetTotal)
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
// 	-30.918273ms -> -31ms
//
func smartFormat(d time.Duration) string {
	if (d > 0 && d < time.Second) || (d < 0 && -d < time.Second) {
		// 30.918273ms -> 31ms
		return d.Round(1 * time.Millisecond).String()
	}
	if (d > 0 && d < time.Minute) || (d < 0 && -d < time.Minute) {
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
		return fmt.Sprintf("%.0f%%", percentageIncrease(float64(before), float64(after)))
	}
	formatPercentageIncreaseFloat := func(before, after float64) string {
		return fmt.Sprintf("%.0f%%", percentageIncrease(before, after))
	}

	w.Write([]string{
		"Name",
		"Queries per second",
		"Duration",
		"Mean bytes sent change",
		"Mean bytes received change",
		"Throughput",
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
		"Throughput before",
		"Throughput after",
		"Total sent bytes before",
		"Total sent bytes after",
		"Mean sent bytes before",
		"Mean sent bytes after",
		"Total received bytes before",
		"Total received bytes after",
		"Mean received bytes before",
		"Mean received bytes after",
	})
	for _, b := range benchmarks {
		before := b.before.Latencies
		after := b.after.Latencies
		w.Write([]string{
			b.name,
			fmt.Sprintf("%.0f", b.after.Rate),
			b.after.Duration.Round(3 * time.Second).String(), // 1m58.999964907s -> 2m0s
			formatPercentageIncreaseFloat(b.before.BytesOut.Mean, b.after.BytesOut.Mean),
			formatPercentageIncreaseFloat(b.before.BytesIn.Mean, b.after.BytesIn.Mean),
			formatPercentageIncreaseFloat(b.before.Throughput, b.after.Throughput),
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

			fmt.Sprint(b.before.Throughput),
			fmt.Sprint(b.after.Throughput),

			fmt.Sprint(b.before.BytesOut.Total),
			fmt.Sprint(b.after.BytesOut.Total),
			fmt.Sprint(b.before.BytesOut.Mean),
			fmt.Sprint(b.after.BytesOut.Mean),

			fmt.Sprint(b.before.BytesIn.Total),
			fmt.Sprint(b.after.BytesIn.Total),
			fmt.Sprint(b.before.BytesIn.Mean),
			fmt.Sprint(b.after.BytesIn.Mean),
		})
	}
}

func writeXLSX(benchmarks []benchmark, requestsTotal, datasetTotal uint64) {
	f := excelize.NewFile()
	sheet := "Sheet1"
	results := f.NewSheet(sheet)
	f.SetActiveSheet(results)
	defer f.Write(os.Stdout)

	f.SetColWidth(sheet, "A", "A", 22)
	f.SetColWidth(sheet, "B", "B", 19)
	f.SetColWidth(sheet, "C", "D", 18)
	f.SetColWidth(sheet, "E", "I", 13)
	f.SetColWidth(sheet, "J", "K", 25)
	f.SetColWidth(sheet, "L", "M", 14)

	bold, _ := f.NewStyle(`{"font":{"bold":true}}`)
	green, _ := f.NewStyle(`{"fill":{"type":"pattern","color":["#29fd2e"],"pattern":1}}`)
	gray, _ := f.NewStyle(`{"fill":{"type":"pattern","color":["#b7b7b7"],"pattern":1}}`)
	lightGray, _ := f.NewStyle(`{"fill":{"type":"pattern","color":["#cccccc"],"pattern":1}}`)
	red, _ := f.NewStyle(`{"fill":{"type":"pattern","color":["#fc0d1b"],"pattern":1}, "font":{"color": "#ffffff"}}`)

	dataSizeStyle, _ := f.NewStyle(`{"custom_number_format": "[<1000000]0.00,\" KB\";[<1000000000]0.00,,\" MB\";0.00,,,\" GB\""}`) // https://stackoverflow.com/a/7239925
	commaNumberStyle, _ := f.NewStyle(`{"custom_number_format": "#,##0"}`)

	f.SetCellValue(sheet, "A1", "Legend")
	f.SetCellStyle(sheet, "A1", "A1", bold)
	f.SetCellValue(sheet, "A2", "Good")
	f.SetCellStyle(sheet, "A2", "A2", green)
	f.SetCellValue(sheet, "A3", "No change")
	f.SetCellStyle(sheet, "A3", "A3", gray)
	f.SetCellValue(sheet, "A4", "Within margin of error")
	f.SetCellStyle(sheet, "A4", "A4", lightGray)
	f.SetCellValue(sheet, "A5", "Individual metric worse")
	f.SetCellStyle(sheet, "A5", "A5", red)
	f.SetCellValue(sheet, "A6", "")

	f.SetCellStyle(sheet, "C1", "C1", bold)
	f.SetCellValue(sheet, "C1", "Dataset total")
	f.SetCellStyle(sheet, "C2", "C2", dataSizeStyle)
	f.SetCellValue(sheet, "C2", datasetTotal)

	f.SetCellStyle(sheet, "D1", "D1", bold)
	f.SetCellValue(sheet, "D1", "Requests total")

	f.SetCellStyle(sheet, "D2", "D2", commaNumberStyle)
	f.SetCellValue(sheet, "D2", requestsTotal)

	row := 7
	f.SetCellValue(sheet, fmt.Sprintf("A%d", row), "Name")
	f.SetCellValue(sheet, fmt.Sprintf("B%d", row), "Total requests change")
	f.SetCellValue(sheet, fmt.Sprintf("C%d", row), "Request rate change")
	f.SetCellValue(sheet, fmt.Sprintf("D%d", row), "Throughput change")
	f.SetCellValue(sheet, fmt.Sprintf("E%d", row), "Mean change")
	f.SetCellValue(sheet, fmt.Sprintf("F%d", row), "P50 change")
	f.SetCellValue(sheet, fmt.Sprintf("G%d", row), "P95 change")
	f.SetCellValue(sheet, fmt.Sprintf("H%d", row), "P99 change")
	f.SetCellValue(sheet, fmt.Sprintf("I%d", row), "Max change")
	f.SetCellValue(sheet, fmt.Sprintf("J%d", row), "Mean bytes sent change")
	f.SetCellValue(sheet, fmt.Sprintf("K%d", row), "Mean bytes received change")
	f.SetCellValue(sheet, fmt.Sprintf("L%d", row), "Success change")
	f.SetCellValue(sheet, fmt.Sprintf("M%d", row), "Test duration")
	f.SetCellStyle(sheet, fmt.Sprintf("A%d", row), fmt.Sprintf("Z%d", row), bold)

	formatPercentageIncrease := func(before, after time.Duration) string {
		return fmt.Sprintf("%.0f%%", percentageIncrease(float64(before), float64(after)))
	}
	formatPercentageIncreaseFloat := func(before, after float64) string {
		return fmt.Sprintf("%.0f%%", percentageIncrease(before, after))
	}
	addComment := func(cell, comment string) {
		d, _ := json.Marshal(struct {
			Author string
			Text   string
		}{
			Author: "Script: ",
			Text:   comment,
		})
		f.AddComment(sheet, cell, string(d))
	}
	for _, b := range benchmarks {
		row++
		before := b.before.Latencies
		after := b.after.Latencies

		equal := func(x, y, margin interface{}) bool {
			switch xx := x.(type) {
			case int:
				yy := y.(int)
				if xx > yy {
					return (xx - yy) < margin.(int)
				}
				return (yy - xx) < margin.(int)
			case uint64:
				yy := y.(uint64)
				if xx > yy {
					return (xx - yy) < margin.(uint64)
				}
				return (yy - xx) < margin.(uint64)
			case time.Duration:
				yy := y.(time.Duration)
				if xx > yy {
					return (xx - yy) < margin.(time.Duration)
				}
				return (yy - xx) < margin.(time.Duration)
			case float64:
				yy := y.(float64)
				if xx > yy {
					return (xx - yy) < margin.(float64)
				}
				return (yy - xx) < margin.(float64)
			default:
				panic("never here")
			}
		}
		greaterThan := func(x, y interface{}) bool {
			switch xx := x.(type) {
			case int:
				return xx > y.(int)
			case uint64:
				return xx > y.(uint64)
			case time.Duration:
				return xx > y.(time.Duration)
			case float64:
				return xx > y.(float64)
			default:
				panic("never here")
			}
		}
		setCellColor := func(cell string, before, after, margin interface{}, moreIsGood bool) {
			moreStyle := green
			lessStyle := red
			if !moreIsGood {
				moreStyle = red
				lessStyle = green
			}
			if reflect.DeepEqual(before, after) {
				f.SetCellStyle(sheet, cell, cell, gray)
			} else if equal(before, after, margin) {
				f.SetCellStyle(sheet, cell, cell, lightGray)
			} else if greaterThan(after, before) {
				f.SetCellStyle(sheet, cell, cell, moreStyle)
			} else {
				f.SetCellStyle(sheet, cell, cell, lessStyle)
			}
		}
		setCellIncrease := func(cell string, before, after, margin time.Duration, moreIsGood bool) {
			f.SetCellValue(sheet, cell, fmt.Sprintf("%s", smartFormat(after-before)))
			addComment(cell, fmt.Sprintf("%v -> %v (%s)", smartFormat(before), smartFormat(after), formatPercentageIncrease(before, after)))
			setCellColor(cell, before, after, margin, moreIsGood)
		}
		setCellIncreaseFloat := func(cell string, before, after, margin float64, nearestDecimal int, unit string, moreIsGood bool) {
			before = round(before, nearestDecimal)
			after = round(after, nearestDecimal)
			fmtString := "%0." + fmt.Sprint(nearestDecimal) + "f"
			f.SetCellValue(sheet, cell, fmt.Sprintf(fmtString+" %s", after-before, unit))
			addComment(cell, fmt.Sprintf(fmtString+" %s -> "+fmtString+" %s (%s)", before, unit, after, unit, formatPercentageIncreaseFloat(before, after)))
			setCellColor(cell, before, after, margin, moreIsGood)
		}

		f.SetCellValue(sheet, fmt.Sprintf("A%d", row), b.name)
		addComment(fmt.Sprintf("A%d", row), fmt.Sprintf("Name: %v", b.name))

		setCellIncreaseFloat(fmt.Sprintf("B%d", row), float64(b.before.Requests), float64(b.after.Requests), float64(*flagTotalRequestsMargin), 0, "requests", true)
		setCellIncreaseFloat(fmt.Sprintf("C%d", row), b.before.Rate, b.after.Rate, 0.0, 0, "requests", true)
		setCellIncreaseFloat(fmt.Sprintf("D%d", row), b.before.Throughput, b.after.Throughput, *flagThroughputMargin, 1, "requests", true)
		setCellIncrease(fmt.Sprintf("E%d", row), before.Mean, after.Mean, *flagRequestDurationMargin, false)
		setCellIncrease(fmt.Sprintf("F%d", row), before.P50, after.P50, *flagRequestDurationMargin, false)
		setCellIncrease(fmt.Sprintf("G%d", row), before.P95, after.P95, *flagRequestDurationMargin, false)
		setCellIncrease(fmt.Sprintf("H%d", row), before.P99, after.P99, *flagRequestDurationMargin, false)
		setCellIncrease(fmt.Sprintf("I%d", row), before.Max, after.Max, *flagRequestDurationMargin, false)
		setCellIncreaseFloat(fmt.Sprintf("J%d", row), b.before.BytesOut.Mean, b.after.BytesOut.Mean, *flagMeanBytesSentMargin, 0, "bytes", true)
		setCellIncreaseFloat(fmt.Sprintf("K%d", row), b.before.BytesIn.Mean, b.after.BytesIn.Mean, *flagMeanBytesReceivedMargin, 0, "bytes", true)
		setCellIncreaseFloat(fmt.Sprintf("L%d", row), b.before.Success*100.0, b.after.Success*100.0, *flagSuccessMargin, 0, "percent", true)

		beforeDuration := b.before.Duration.Round(3 * time.Second).String() // 1m58.999964907s -> 2m0s
		afterDuration := b.after.Duration.Round(3 * time.Second).String()
		f.SetCellValue(sheet, fmt.Sprintf("M%d", row), afterDuration)
		addComment(fmt.Sprintf("M%d", row), fmt.Sprintf("%v -> %v (%s)", beforeDuration, afterDuration, formatPercentageIncrease(b.before.Duration, b.after.Duration)))
	}
}

// round rounds x to the nth decimal place, e.g. round(0.123456, 2) == 0.12
func round(x float64, n int) float64 {
	unit := 0.5 * (float64(n) + 1)
	return math.Round(x/unit) * unit
}

func writeMarkdown(benchmarks []benchmark) {
	// Go over each file and get and compare the before/after metrics.
	for _, b := range benchmarks {
		// Helper function for formatting the duration difference strings.
		formatDurationDifference := func(before, after time.Duration) string {
			return fmt.Sprintf("%v → %v (%.2f%%)",
				before.Round(time.Millisecond),
				after.Round(time.Millisecond),
				percentageIncrease(float64(before), float64(after)),
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
