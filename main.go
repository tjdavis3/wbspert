package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	flags "github.com/jessevdk/go-flags"
	"github.com/jszwec/csvutil"
)

type cfg struct {
	Input  string `short:"i" default:"-" description:"The input file or - for stdin"`
	Output string `short:"o" default:"-" description:"The output file or - for stdout"`
	Level  int    `short:"l" default:"3" description:"The WBS level to use for PERT charts"`
	WBS    bool   `short:"w"  description:"Generate the WBS"`
	PERT   bool   `short:"p"  description:"Generate the PERT"`
	Table  bool   `short:"t" description:"Generate Markdown Table"`
	Embed  bool   `short:"e" description:"Embed in an existing file"`
}

type Sheet struct {
	WBS      string  `csv:"Task"`
	Title    string  `csv:"Title"`
	Parents  string  `csv:"Parents"`
	Duration float32 `csv:"Duration,omitempty"`
	Status   string  `csv:"Status"`
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
const markDownRow = "| %s | %s | %s | %s |"

const wbsEmbed = `(?m:^ *)<!--\s*wbs:embed:start\s*-->(?s:.*?)<!--\s*wbs:embed:end\s*-->(?m:\s*?$)`
const wbsTableEmbed = `(?m:^ *)<!--\s*wbsTable:embed:start\s*-->(?s:.*?)<!--\s*wbsTable:embed:end\s*-->(?m:\s*?$)`
const pertEmbed = `(?m:^ *)<!--\s*pert:embed:start\s*-->(?s:.*?)<!--\s*pert:embed:end\s*-->(?m:\s*?$)`

var (
	wbsRegex      = regexp.MustCompile(wbsEmbed)
	wbsTableRegex = regexp.MustCompile(wbsTableEmbed)
	pertRegex     = regexp.MustCompile(pertEmbed)
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

// GetPertNode returns a PlantUML string that represents
// the task in a PERT chart
func (s *Sheet) GetPertNode() string {
	color := s.GetStatusColor()
	return fmt.Sprintf(pertNode, s.WBS, s.Title, s.WBS, color, s.Status, s.Duration)
}

// GetPertLevel returns the PlantUML PERT node if the WBS task
// is at least the level specified.  Otherwise an empty string
// is returned.
func (s *Sheet) GetPertLevel(lvl int) string {
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
	return fmt.Sprintf(markDownRow, s.WBS, s.Title, s.Parents, strconv.FormatFloat(float64(s.Duration), 'f', 2, 32))
}

func genMarkdownTableHeader() string {
	return strings.Join([]string{
		fmt.Sprintf(markDownRow, "WBS", "Task", "Parents", "Duration"),
		fmt.Sprintf(markDownRow, "---", "----", "-------", "--------"),
	}, "\n")
}

func main() {
	config := &cfg{}
	_, err := flags.Parse(config)
	if err != nil {
		log.Fatal(err)
	}
	var in *os.File
	var out *os.File
	if config.Input == "-" {
		in = os.Stdin
	} else {
		in, err = os.Open(config.Input)
		defer in.Close()
	}
	if config.Output == "-" {
		out = os.Stdout
	} else {
		out, err = os.Create(config.Output)
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()
	}

	if config.PERT {
		PertChart(in, out, config)
		_, err := in.Seek(0, 0)
		if err != nil {
			log.Fatal(err)
		}
	}
	if config.WBS {
		WBS(in, out, config)
		in.Seek(0, 0)
	}
	if config.Table {
		WBSTable(in, out, config)
	}

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

func PertChart(in io.Reader, out io.StringWriter, config *cfg) {
	var allParents []string
	var tasks []string
	decoder := buildDecoder(in)
	out.WriteString("@startuml PERT\n")
	out.WriteString("left to right direction\n")
	out.WriteString("map Start {\n}\n")
	out.WriteString("map Finish {\n}\n")

	var edges []string
	for {
		var sheet Sheet
		if err := decoder.Decode(&sheet); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		out.WriteString(sheet.GetPertLevel(config.Level))
		if sheet.GetLevel() >= config.Level {
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
}

func WBS(in io.Reader, out io.StringWriter, config *cfg) {
	decoder := buildDecoder(in)
	out.WriteString("@startwbs\n")
	out.WriteString("* Project\n")
	for {
		var sheet Sheet
		if err := decoder.Decode(&sheet); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		out.WriteString(sheet.GetWBSLevel(config.Level))
		out.WriteString("\n")
	}
	out.WriteString("\nfooter\nAs of %date()\nend footer\n")
	out.WriteString(legend)
	out.WriteString("@endwbs\n")
}

func WBSTable(in io.Reader, out io.StringWriter, config *cfg) {
	decoder := buildDecoder(in)
	out.WriteString(genMarkdownTableHeader())
	out.WriteString("\n")
	for {
		var sheet Sheet
		if err := decoder.Decode(&sheet); err == io.EOF {
			break
		} else if err != nil {
			log.Fatal(err)
		}
		out.WriteString(sheet.MarkdownRow())
		out.WriteString("\n")
	}
}
