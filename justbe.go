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
	"github.com/gabriel-vasile/mimetype"
	"github.com/jessevdk/go-flags"

	mymazda "github.com/taylormonacelli/forestfish/mymazda"
)

var opts struct {
	LogFormat string `long:"log-format" choice:"text" choice:"json" default:"text" description:"Log format"`
	Verbose   []bool `short:"v" long:"verbose" description:"Show verbose debug information, each -v bumps log level"`
	logLevel  slog.Level
	Paths     []string `short:"p" long:"path" description:"File paths to be processed" required:"true"`

	ReportMatches    bool `short:"m" long:"report-matches" description:"Generate report for matched lines"`
	ReportStats      bool `short:"s" long:"report-stats" description:"Generate statistics report"`
	ReportNameCounts bool `short:"n" long:"report-name-counts" description:"Generate report for name counts"`
}

type MatchedLine struct {
	FilePath    string
	LineNumber  int
	Name        string
	IndentLevel int
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

	err = CanProcessFiles(expandedPaths...)
	if err != nil {
		return fmt.Errorf("error asserting text files: %v", err)
	}

	var matches []MatchedLine

	// build matches from paths
	for _, path := range expandedPaths {
		if err := processFile(path, &matches); err != nil {
			return fmt.Errorf("error processing file %s: %v", path, err)
		}
	}

	if opts.ReportMatches {
		reportMatches, err := genReportMatches(matches)
		if err != nil {
			return fmt.Errorf("error printing matches: %v", err)
		}
		fmt.Println(reportMatches)
	}

	if opts.ReportNameCounts {
		reportNameCounts, err := genReportNameCounts(matches)
		if err != nil {
			return fmt.Errorf("error printing name counts: %v", err)
		}
		fmt.Println(reportNameCounts)
	}

	if opts.ReportStats {
		reportStats, err := genReportStats(matches, expandedPaths)
		if err != nil {
			return fmt.Errorf("error printing stats: %v", err)
		}
		fmt.Println(reportStats)
	}

	return nil
}

func CanProcessFiles(paths ...string) error {
	for _, path := range paths {
		mimetype, err := mimetype.DetectFile(path)
		if err != nil {
			return fmt.Errorf("error detecting mimetype of file %s: %v", path, err)
		}

		if mimetype.String() != "text/plain; charset=utf-8" {
			return fmt.Errorf("file %s is not a text file", path)
		}
	}

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

func genReportMatches(matches []MatchedLine) (string, error) {
	sortedMatches := make([]MatchedLine, len(matches))
	copy(sortedMatches, matches)
	sortMatchesByName(sortedMatches)

	matchesTemplate := `
{{range $index, $match := .}}
{{printf "%5s. %s %s:%d" (formatNumWithCommas $index) $match.Name $match.FilePath $match.LineNumber}}{{end}}
`

	tmpl, err := template.New("matches").Funcs(funcMap).Parse(matchesTemplate)
	if err != nil {
		return "", fmt.Errorf("error creating template: %v", err)
	}

	var b strings.Builder
	err = tmpl.Execute(&b, sortedMatches)
	if err != nil {
		return "", fmt.Errorf("error executing template: %v", err)
	}

	return b.String(), nil
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

	lineCount, err := countLines(path)
	if err != nil {
		slog.Warn("error counting lines in file %s: %v", path, err)
		return 0, fmt.Errorf("error counting lines in file %s: %v", path, err)
	}

	return lineCount, nil
}

func genReportStats(matches []MatchedLine, paths []string) (string, error) {
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
		return "", fmt.Errorf("error creating template: %v", err)
	}

	var b strings.Builder
	err = tmpl.Execute(&b, statsData)
	if err != nil {
		return "", fmt.Errorf("error executing template: %v", err)
	}

	return b.String(), nil
}

func countLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("error opening file %s: %v", path, err)
	}

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

func genReportNameCounts(matches []MatchedLine) (string, error) {
	type NameInfo struct {
		Name   string
		Count  int
		Places []string
	}

	nameCount := make(map[string]NameInfo)

	for _, match := range matches {
		lowerName := strings.ToLower(match.Name)
		info, found := nameCount[lowerName]
		if !found {
			info = NameInfo{Name: match.Name}
		}

		info.Count++
		info.Places = append(info.Places, fmt.Sprintf("%s:%d", match.FilePath, match.LineNumber))

		nameCount[lowerName] = info
	}

	names := make([]NameInfo, 0, len(nameCount))
	for _, info := range nameCount {
		names = append(names, info)
	}

	sort.Slice(names, func(i, j int) bool {
		return names[i].Count > names[j].Count
	})

	filteredNames := make([]NameInfo, 0)

	for _, info := range names {
		if info.Count >= 2 {
			filteredNames = append(filteredNames, info)
		}
	}

	sort.Slice(filteredNames, func(i, j int) bool {
		return filteredNames[i].Count > filteredNames[j].Count
	})

	const namesTemplate = `
Name duplicates (>= 2), total: {{ formatNumWithCommas .TotalDuplicates }}
{{- range .Names }}
{{ .Name }}: {{ .Count }}
{{ range .Places -}}
    {{ . }}
{{ end -}}
{{ end -}}
`

	tmpl, err := template.New("names").Funcs(funcMap).Parse(namesTemplate)
	if err != nil {
		return "", fmt.Errorf("error creating template: %v", err)
	}

	namesData := struct {
		Names           []NameInfo
		TotalDuplicates int
	}{
		Names:           filteredNames,
		TotalDuplicates: len(filteredNames),
	}

	var b strings.Builder
	err = tmpl.Execute(&b, namesData)
	if err != nil {
		return "", fmt.Errorf("error executing template: %v", err)
	}

	return b.String(), nil
}
