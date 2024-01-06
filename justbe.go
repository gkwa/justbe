package justbe

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/dustin/go-humanize"
	"github.com/jessevdk/go-flags"
	mymazda "github.com/taylormonacelli/forestfish/mymazda"
)

var opts struct {
	LogFormat string `long:"log-format" choice:"text" choice:"json" default:"text" description:"Log format"`
	Verbose   []bool `short:"v" long:"verbose" description:"Show verbose debug information, each -v bumps log level"`
	logLevel  slog.Level
	Paths     []string `short:"p" long:"path" description:"File paths to be processed" required:"true"`
}

func Execute() int {
	if err := parseFlags(); err != nil {
		return 1
	}

	if err := setLogLevel(); err != nil {
		return 1
	}

	if err := setupLogger(); err != nil {
		return 1
	}

	err := run(opts.Paths)
	if err != nil {
		slog.Error("run failed", "error", err)
		return 1
	}

	return 0
}

func parseFlags() error {
	_, err := flags.Parse(&opts)
	return err
}

func run(paths []string) error {
	expandedPaths, err := tildeExpandPaths(paths...)
	if err != nil {
		return fmt.Errorf("error expanding paths: %v", err)
	}

	var matches []MatchedLine

	for _, path := range expandedPaths {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("error opening file %s: %v", path, err)
		}
		defer file.Close()

		if err := processFile(file, path, &matches); err != nil {
			return fmt.Errorf("error processing file %s: %v", path, err)
		}
	}

	printMatches(matches)
	printStats(matches)

	return nil
}

func processFile(file *os.File, path string, matches *[]MatchedLine) error {
	scanner := bufio.NewScanner(file)
	lineNumber := 0

	pattern := regexp.MustCompile(`(?i)^(\*+)\s+(.*)\s+tidbits$`)

	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()

		if submatches := pattern.FindStringSubmatch(line); len(submatches) > 1 {
			indentLevel := len(submatches[1])
			name := strings.TrimSpace(submatches[2])
			matchedLine := MatchedLine{
				FilePath:    path,
				LineNumber:  lineNumber,
				Name:        name,
				IndentLevel: indentLevel,
			}
			*matches = append(*matches, matchedLine)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading file %s: %v", path, err)
	}

	return nil
}

func printMatches(matches []MatchedLine) {
	sortedMatches := make([]MatchedLine, len(matches))
	copy(sortedMatches, matches)
	sortMatchesByName(sortedMatches)

	for index, match := range sortedMatches {
		fmt.Printf("%s %s %s:%d\n", humanize.Comma(int64(index)), match.Name, match.FilePath, match.LineNumber)
	}
}

func sortMatchesByName(matches []MatchedLine) {
	sort.Slice(matches, func(i, j int) bool {
		return strings.ToLower(matches[i].Name) < strings.ToLower(matches[j].Name)
	})
}

func printStats(matches []MatchedLine) {
	fileLineCounts := make(map[string]int)
	fileMatchedLineCounts := make(map[string]int)
	totalLineCount := 0
	totalMatchedLineCount := 0

	for _, match := range matches {
		fileLineCounts[match.FilePath]++
		fileMatchedLineCounts[match.FilePath]++
		totalLineCount++
		totalMatchedLineCount++
	}

	for _, path := range opts.Paths {
		file, err := os.Open(path)
		if err != nil {
			slog.Warn("error opening file %s: %v", path, err)
			continue
		}
		defer file.Close()

		lineCount, err := countLines(file)
		if err != nil {
			slog.Warn("error counting lines in file %s: %v", path, err)
			continue
		}

		fileLineCounts[path] = lineCount
		totalLineCount += lineCount
	}

	fmt.Println("\nFile Line Counts:")
	for path, count := range fileLineCounts {
		fmt.Printf("%s: %s\n", path, humanize.Comma(int64(count)))
	}

	fmt.Printf("\nTotal Line Count: %s\n", humanize.Comma(int64(totalLineCount)))

	fmt.Println("\nFile Matched Line Counts:")
	for path, count := range fileMatchedLineCounts {
		fmt.Printf("%s: %s\n", path, humanize.Comma(int64(count)))
	}

	fmt.Printf("\nTotal Matched Line Count: %s\n", humanize.Comma(int64(totalMatchedLineCount)))
}

func countLines(file *os.File) (int, error) {
	scanner := bufio.NewScanner(file)
	lineCount := 0

	for scanner.Scan() {
		lineCount++
	}

	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("error counting lines: %v", err)
	}

	return lineCount, nil
}

func tildeExpandPaths(paths ...string) ([]string, error) {
	var expandedPaths []string

	for _, path := range paths {
		path, err := mymazda.ExpandTilde(path)
		if err != nil {
			return []string{}, fmt.Errorf("error expanding home directory in path %s: %v", path, err)
		}
		expandedPaths = append(expandedPaths, path)
	}

	return expandedPaths, nil
}

type MatchedLine struct {
	FilePath    string
	LineNumber  int
	Name        string
	IndentLevel int
}
