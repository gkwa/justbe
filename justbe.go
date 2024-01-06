package justbe

import (
	"bufio"
	"fmt"
	"html/template"
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

func formatNumWithCommas(num int) string {
	return humanize.Comma(int64(num))
}

var funcMap = template.FuncMap{
	"formatNumWithCommas": formatNumWithCommas,
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
	expandedPaths, err := getAbsPath(paths...)
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

		if err := processFile(path, &matches); err != nil {
			return fmt.Errorf("error processing file %s: %v", path, err)
		}
	}

	printMatches(matches)
	printStats(matches, expandedPaths)

	return nil
}

func processFile(path string, matches *[]MatchedLine) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("error opening file %s: %v", path, err)
	}

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

	matchesTemplate := `
{{range $index, $match := .}}
{{printf "%5s. %s %s:%d" (formatNumWithCommas $index) $match.Name $match.FilePath $match.LineNumber}}{{end}}
`

	tmpl, err := template.New("matches").Funcs(funcMap).Parse(matchesTemplate)
	if err != nil {
		slog.Error("error creating template: %v", err)
		return
	}

	err = tmpl.Execute(os.Stdout, sortedMatches)
	if err != nil {
		slog.Error("error executing template: %v", err)
		return
	}
}

func sortMatchesByName(matches []MatchedLine) {
	sort.Slice(matches, func(i, j int) bool {
		return strings.ToLower(matches[i].Name) < strings.ToLower(matches[j].Name)
	})
}

func countLinesInFile(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		slog.Warn("error opening file %s: %v", path, err)
		return 0, fmt.Errorf("error opening file %s: %v", path, err)
	}
	defer file.Close()

	lineCount, err := countLines(file)
	if err != nil {
		slog.Warn("error counting lines in file %s: %v", path, err)
		return 0, fmt.Errorf("error counting lines in file %s: %v", path, err)
	}

	return lineCount, nil
}

func printStats(matches []MatchedLine, paths []string) {
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

	for _, path := range paths {
		lineCount, err := countLinesInFile(path)
		if err != nil {
			continue
		}

		fileLineCounts[path] = lineCount
		totalLineCount += lineCount
	}

	statsData := struct {
		FileLineCounts        map[string]int
		TotalLineCount        int
		FileMatchedLineCounts map[string]int
		TotalMatchedLineCount int
	}{
		FileLineCounts:        fileLineCounts,
		TotalLineCount:        totalLineCount,
		FileMatchedLineCounts: fileMatchedLineCounts,
		TotalMatchedLineCount: totalMatchedLineCount,
	}

	statsTemplate := `
File Line Counts:
{{range $path, $count := .FileLineCounts}}{{printf "%10s: %s\n"  (formatNumWithCommas $count) $path}}{{end}}
{{printf "%10s" (formatNumWithCommas .TotalLineCount)}}: Total Line Count
{{range $path, $count := .FileMatchedLineCounts}}{{printf "%10s: %s: File Matched Line Counts"  (formatNumWithCommas $count) $path}}
{{end}}
{{ printf "%10s" (formatNumWithCommas .TotalMatchedLineCount)}}: Total Matched Line Count
`

	tmpl, err := template.New("stats").Funcs(funcMap).Parse(statsTemplate)
	if err != nil {
		slog.Error("error creating template: %v", err)
		return
	}

	err = tmpl.Execute(os.Stdout, statsData)
	if err != nil {
		slog.Error("error executing template: %v", err)
		return
	}
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

func getAbsPath(paths ...string) ([]string, error) {
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
