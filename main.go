package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"ghprojects/projects"

	flags "github.com/jessevdk/go-flags"
	"github.com/jinzhu/copier"
	"github.com/jszwec/csvutil"
)

type cfg struct {
	Input       string `short:"i" default:"-" description:"The input file or - for stdin"`
	Output      string `short:"o" default:"-" description:"The output file or - for stdout"`
	Level       int    `short:"l" default:"3" description:"The WBS level to use for PERT charts"`
	WBS         bool   `short:"w"  description:"Generate the WBS"`
	PERT        bool   `short:"p"  description:"Generate the PERT"`
	Table       bool   `short:"t" description:"Generate Markdown Table"`
	Embed       bool   `short:"e" description:"Embed in an existing file"`
	Token       string `long:"token" env:"GITHUB_TOKEN" long:"github-token" description:"Access token for calling Github API"`
	Org         string `long:"org" default:"ringsq" description:"Github org containing the project"`
	Project     string `short:"j" long:"project" description:"Github Project name"`
	ByRepo      bool   `short:"r" description:"Do WBS by repo name"`
	Kanban      bool   `short:"k" description:"Build a kanban table"`
	Column      string `short:"c" default:"Status" description:"Column field for Kanban table"`
	BugList     bool   `short:"b" description:"Generate a buglist"`
	EpicList    bool   `short:"E" long:"epiclist" description:"Generate a checklist of epics"`
	ActiveOnly  bool   `short:"a" description:"Only show incomplete tasks"`
	EpicDir     string `short:"d" description:"The location to write epic stories"`
	EpicStories bool   `short:"s" description:"Write epic stories"`
	Filter      string `short:"f" long:"filter" description:"Filter WBS Table and Kanban by a label value"`
}

type Sheet struct {
	WBS      string            `csv:"Task"`
	Title    string            `csv:"Title"`
	Parents  string            `csv:"Parents"`
	Duration float32           `csv:"Duration,omitempty"`
	Status   string            `csv:"Status"`
	Labels   []string          `csv:"omitempty"`
	Fields   map[string]string `csv:"omitempty"`
	Repo     string            `csv:"omitempty"`
	Body     string            `csv:"omitempty"`
	Number   int               `csv:"omitempty"`
}

const pertNode = `
map "%s: %s" as %s %s {
	Status => %s
	Early => ES:   | EF:    
	Duration => %0.1f
	Late  => LS:   | LF:     
}
`
const legend = `
legend right
	<size:18><u>Legend</u></size>
	<back:Thistle>Complete</back>
	<back:DarkSeaGreen>In Process</back>
	<back:Pink>Waiting on Someone</back>
	<back:Red>Blocked / Stalled</back>
	<back:Orange>Milestone</back>
end legend
`
const markDownRow = "| %s | %s | %s | %s | %s |"

const (
	wbsTag      = "wbs"
	wbsTableTag = "wbsTable"
	pertTag     = "pert"
	kanbanTag   = "kanban"
	bugTag      = "bug"
	epicTag     = "epic"
)

var wbsEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, wbsTag, wbsTag)
var wbsTableEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, wbsTableTag, wbsTableTag)
var pertEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, pertTag, pertTag)
var kanbanEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, kanbanTag, kanbanTag)
var bugEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, bugTag, bugTag)
var epicEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, epicTag, epicTag)

var (
	wbsRegex      = regexp.MustCompile(wbsEmbed)
	wbsTableRegex = regexp.MustCompile(wbsTableEmbed)
	pertRegex     = regexp.MustCompile(pertEmbed)
	kanbanRegex   = regexp.MustCompile(kanbanEmbed)
	bugRegex      = regexp.MustCompile(bugEmbed)
	epicRegex     = regexp.MustCompile(epicEmbed)
)

// GetParents splits the parents and returns
// them as a list of strings
func (s *Sheet) GetParents() []string {
	parents := strings.Split(s.Parents, ",")
	for i, p := range parents {
		parents[i] = strings.Trim(p, " ")
	}
	return parents
}

func (s *Sheet) GetStatusColor() string {
	color := ""
	switch strings.ToLower(s.Status) {
	case "under review":
		fallthrough
	case "in progress":
		color = "#DarkSeaGreen"
	case "complete":
		fallthrough
	case "done":
		color = "#Thistle"
	case "blocked":
		fallthrough
	case "stalled":
		color = "#Red"
	case "waiting":
		color = "#Pink"
	case "milestone":
		color = "#Orange"
	}
	return color
}

func (s *Sheet) IsCompleted() bool {
	status := strings.ToLower(s.Status)
	if status == "done" || strings.HasPrefix(status, "complete") {
		return true
	}
	return false
}

// GetPertNode returns a PlantUML string that represents
// the task in a PERT chart
func (s *Sheet) GetPertNode() string {
	color := s.GetStatusColor()
	return fmt.Sprintf(pertNode, s.WBS, strings.ReplaceAll(s.Title, `"`, ""), s.WBS, color, s.Status, s.Duration)
}

// GetPertLevel returns the PlantUML PERT node if the WBS task
// is at least the level specified.  Otherwise an empty string
// is returned.
func (s *Sheet) GetPertLevel(lvl int) string {
	if s.Status == "" {
		return ""
	}
	if s.GetLevel() >= lvl {
		return s.GetPertNode()
	}
	return ""
}

// GetLevel returns the WBS level for this task based on the
// task ID.
func (s *Sheet) GetLevel() int {
	return strings.Count(s.WBS, ".") + 1
}

// GetWBS returns a PlantUML WBS line for the task
func (s *Sheet) GetWBS() string {
	return s.GetWBSLevel(0)
}

func (s *Sheet) GetWBSLevel(lvl int) string {
	printlvl := s.GetLevel()
	if printlvl == 1 {
		printlvl = 2
	}
	str := fmt.Sprintf("%s", strings.Repeat("*", printlvl))
	color := s.GetStatusColor()
	if len(color) > 0 {
		str = fmt.Sprintf("%s[%s]", str, color)
	}
	if s.GetLevel() > lvl && lvl > 0 {
		str = str + "_"
	}
	str = fmt.Sprintf("%s %s: %s", str, s.WBS, s.Title)
	return str
}

// MarkdownRow returns a markdown table row representing the task
func (s *Sheet) MarkdownRow() string {
	title := s.Title
	if strings.ToLower(s.Status) == "done" || strings.ToLower(s.Status) == "complete" {
		title = "~~" + title + "~~"
	}
	return fmt.Sprintf(markDownRow, s.WBS, s.Status, title, s.Parents, strconv.FormatFloat(float64(s.Duration), 'f', 2, 32))
}

func genMarkdownTableHeader() string {
	return strings.Join([]string{
		fmt.Sprintf(markDownRow, "WBS", "Status", "Task", "Parents", "Duration"),
		fmt.Sprintf(markDownRow, "---", "------", "----", "-------", "--------"),
	}, "\n")
}

func main() {
	var sheets []Sheet
	var board *projects.Board
	config := &cfg{}
	_, err := flags.Parse(config)
	if err != nil {
		log.Fatal(err)
	}
	var in *os.File
	var out *os.File
	if config.Input == "-" {
		in = os.Stdin
	} else if config.Input == "gh" {

		client := projects.NewClient(context.Background(), config.Token)
		board, err = client.GetProject(config.Org, config.Project)
		if err != nil {
			log.Fatal(err)
		}
		var wbs []*projects.Card
		if config.ByRepo {
			wbs = board.GetRepoWBS()
		} else {
			wbs = board.GetWBSCards()
		}
		if err := copier.Copy(&sheets, wbs); err != nil {
			log.Fatal(err)
		}

	} else {
		in, err = os.Open(config.Input)
		sheets = readFile(in)
		in.Close()
	}
	if config.Output == "-" {
		out = os.Stdout
	} else {
		if config.Embed {
			out, err = os.OpenFile(config.Output, os.O_RDWR|os.O_CREATE, os.ModePerm)
		} else {
			out, err = os.Create(config.Output)
		}
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()
	}

	if config.PERT {
		PertChart(sheets, out, config)
	}
	if config.WBS {
		WBS(sheets, out, config)
	}
	if config.Table {
		WBSTable(sheets, out, config)
	}

	if config.Kanban {
		Kanban(board, out, config)
	}

	if config.BugList {
		BugList(sheets, out, config)
	}

	if config.EpicList {
		EpicList(sheets, out, config)
	}

	if config.EpicStories {
		EpicStories(sheets, config)
	}

}

func readFile(in io.Reader) []Sheet {
	var sheets []Sheet
	decoder := buildDecoder(in)
	for {
		var sheet Sheet
		if err := decoder.Decode(&sheet); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		sheets = append(sheets, sheet)
	}
	return sheets
}

func buildDecoder(in io.Reader) *csvutil.Decoder {
	csvReader := csv.NewReader(in)
	decoder, err := csvutil.NewDecoder(csvReader)
	if err != nil {
		log.Fatal(err)
	}
	return decoder
}

func inArray(fld string, arr []string) bool {
	for _, key := range arr {
		if fld == key {
			return true
		}
	}
	return false
}

func PertChart(sheets []Sheet, outfile *os.File, config *cfg) {
	var allParents []string
	var tasks []string
	out := bytes.NewBufferString("")
	out.WriteString("@startuml PERT\n")
	out.WriteString("left to right direction\n")
	out.WriteString("map Start {\n}\n")
	out.WriteString("map Finish {\n}\n")

	var edges []string
	for _, sheet := range sheets {
		if strings.HasPrefix(sheet.WBS, "0.99") {
			continue
		}
		if config.ActiveOnly && sheet.IsCompleted() {
			continue
		}
		out.WriteString(sheet.GetPertLevel(config.Level))
		if sheet.GetLevel() >= config.Level && !(sheet.Status == "") {
			tasks = append(tasks, sheet.WBS)
			allParents = append(allParents, sheet.GetParents()...)
			for _, p := range sheet.GetParents() {
				if p == "" {
					p = "Start"
				}
				edges = append(edges, fmt.Sprintf("%s --> %s\n", p, sheet.WBS))
			}
		}
	}
	for _, edge := range edges {
		out.WriteString(edge)
	}
	for _, task := range tasks {
		if !inArray(task, allParents) {
			out.WriteString(fmt.Sprintf("%s --> Finish\n", task))
		}
	}
	out.WriteString("\nfooter\nAs of %date()\nend footer\n")
	out.WriteString(legend)
	out.WriteString("@enduml\n")
	if config.Embed && config.Output != "-" {
		embedContents(outfile, fmt.Sprintf("```plantuml\n%s\n```\n", out.String()), pertRegex, pertTag)
	} else {
		outfile.WriteString(out.String())
	}
}
func FilterCards(columns []*projects.BoardColumn, filter string) []*projects.BoardColumn {
	for _, column := range columns {
		newCards := make([]*projects.Card, 0)
		for _, card := range column.Cards {
			if !(inArray(filter, card.Labels) || card.Fields["Type"] == filter) {
				continue
			}
			newCards = append(newCards, card)
		}
		column.Cards = newCards
	}
	return columns
}
func Kanban(board *projects.Board, outfile *os.File, config *cfg) {
	var rows [][]string
	out := bytes.NewBufferString("")
	if config.Column != "Status" {
		board.SetCards(config.Column)
	}

	if len(config.Filter) > 0 {
		FilterCards(board.Columns, config.Filter)
	}
	maxRows := determineRows(board.Columns)
	rows = make([][]string, maxRows)
	for i := range rows {
		rows[i] = make([]string, len(board.Columns))
	}

	for _, col := range board.Columns {
		fmt.Fprintf(out, "| %s ", col.Name)
	}
	fmt.Fprintln(out, "|")
	for i := 0; i < len(board.Columns); i++ {
		fmt.Fprint(out, "| --- ")
	}
	fmt.Fprintln(out, "|")

	for colNum, curCol := range board.Columns {
		for colRow, card := range curCol.Cards {
			complete := ""
			if card.IsCompleted() {
				if config.ActiveOnly {
					continue
				}
				complete = "~~"
			}

			rows[colRow][colNum] = fmt.Sprintf("%s%s%s", complete, card.Title, complete)
		}
	}
	for _, row := range rows {
		for _, col := range row {
			fmt.Fprintf(out, "| %s ", col)
		}
		fmt.Fprintln(out, "|")
	}
	if config.Embed && config.Output != "-" {
		embedContents(outfile, out.String(), kanbanRegex, kanbanTag)
	} else {
		outfile.WriteString(out.String())
	}
}

func determineRows(cols []*projects.BoardColumn) int {
	var maxRows int
	for _, column := range cols {
		if len(column.Cards) > maxRows {
			maxRows = len(column.Cards)
		}
	}
	return maxRows
}

func WBS(sheets []Sheet, outfile *os.File, config *cfg) {
	out := bytes.NewBufferString("")

	out.WriteString("@startwbs\n")
	out.WriteString("* Project\n")
	for _, sheet := range sheets {
		if config.ActiveOnly && sheet.IsCompleted() {
			continue
		}
		out.WriteString(sheet.GetWBSLevel(99))
		out.WriteString("\n")
	}
	out.WriteString("\nfooter\nAs of %date()\nend footer\n")
	out.WriteString(legend)
	out.WriteString("@endwbs\n")
	if config.Embed && config.Output != "-" {
		embedContents(outfile, fmt.Sprintf("```plantuml\n%s\n```\n", out.String()), wbsRegex, wbsTag)
	} else {
		outfile.WriteString(out.String())
	}

}

func WBSTable(sheets []Sheet, outfile *os.File, config *cfg) {
	out := bytes.NewBufferString("")
	out.WriteString(genMarkdownTableHeader())
	out.WriteString("\n")
	for _, sheet := range sheets {
		if config.ActiveOnly && sheet.IsCompleted() {
			continue
		}
		if len(config.Filter) > 0 {
			if !(inArray(config.Filter, sheet.Labels) || sheet.Fields["Type"] == config.Filter) {
				continue
			}
		}
		out.WriteString(sheet.MarkdownRow())
		out.WriteString("\n")
	}
	if config.Embed && config.Output != "-" {
		embedContents(outfile, out.String(), wbsTableRegex, wbsTableTag)
	} else {
		outfile.WriteString(out.String())
	}
}

func BugList(sheets []Sheet, outfile *os.File, config *cfg) {
	out := bytes.NewBufferString("")
	out.WriteString("| Repo | Status | Title |\n")
	out.WriteString("| --- | --- | --- |\n")
	for _, sheet := range sheets {
		if config.ActiveOnly && sheet.IsCompleted() {
			continue
		}
		if inArray("bug", sheet.Labels) {
			out.WriteString(fmt.Sprintf("| %s | %s | %s |\n", sheet.Repo, sheet.Status, sheet.Title))
		}
	}
	if config.Embed && config.Output != "-" {
		embedContents(outfile, out.String(), bugRegex, bugTag)
	} else {
		outfile.WriteString(out.String())
	}
}

func EpicList(sheets []Sheet, outfile *os.File, config *cfg) {
	out := bytes.NewBufferString("")

	for _, sheet := range sheets {
		if inArray("epic", sheet.Labels) || sheet.Fields["Type"] == "epic" {
			complete := " "
			status := strings.ToLower(sheet.Status)
			if status == "complete" || status == "done" {
				complete = "x"
			}
			out.WriteString(fmt.Sprintf("- [%s] %s\n", complete, sheet.Title))
		}
	}
	if config.Embed && config.Output != "-" {
		embedContents(outfile, out.String(), epicRegex, epicTag)
	} else {
		outfile.WriteString(out.String())
	}
}

var epicHeader = `---
title: "%s: %s"
linkTitle: %s
---

`

func EpicStories(sheets []Sheet, config *cfg) {
	if _, err := os.Stat(config.EpicDir); os.IsNotExist(err) {
		log.Fatalf("EpicDir doesn't exist: %s", config.EpicDir)
	}
	for _, sheet := range sheets {
		if inArray("epic", sheet.Labels) || sheet.Fields["Type"] == "epic" {
			out, err := os.Create(path.Join(config.EpicDir, fmt.Sprintf("%s.md", sheet.WBS)))
			if err != nil {
				log.Fatalf("Error Opening file: %s", err)
			}
			out.WriteString(fmt.Sprintf(epicHeader, sheet.WBS, sheet.Title, sheet.WBS))
			out.WriteString(fmt.Sprintf("**Status:** %s \n", sheet.Status))
			//out.WriteString(fmt.Sprintf("| *Repository* | %s |\n\n", sheet.Repo))
			out.WriteString("\n")
			out.WriteString(sheet.Body)
			out.Close()
		}
	}
}

func embedContents(file *os.File, text string, re *regexp.Regexp, tag string) {
	embedText := fmt.Sprintf("<!-- %s:embed:start -->\n\n%s\n<!-- %s:embed:end -->\n", tag, text, tag)

	file.Seek(0, 0)

	data, err := io.ReadAll(file)
	if err != nil {
		// log.Printf("unable to find output file %s for embedding. Creating a new file instead", file)
		// return embedText
		log.Fatal(err)
	}

	var replacements int
	data = re.ReplaceAllFunc(data, func(_ []byte) []byte {
		replacements++
		return []byte(embedText)
	})

	if replacements == 0 {
		// log.Printf("no embed markers found. Appending documentation to the end of the file instead")
		data = []byte(fmt.Sprintf("%s\n\n%s", string(data), embedText))
	}

	file.Seek(0, 0)
	if err = file.Truncate(0); err != nil {
		log.Panic(err)
	}
	file.Write(data)
}
