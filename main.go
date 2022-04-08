package main

import (
	"bytes"
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

const (
	wbsTag      = "wbs"
	wbsTableTag = "wbsTable"
	pertTag     = "pert"
)

var wbsEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, wbsTag, wbsTag)
var wbsTableEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, wbsTableTag, wbsTableTag)
var pertEmbed = fmt.Sprintf(`(?m:^ *)<!--\s*%s:embed:start\s*-->(?s:.*?)<!--\s*%s:embed:end\s*-->(?m:\s*?$)`, pertTag, pertTag)

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
	var sheets []Sheet
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
	if config.Embed && config.Output != "-" {
		embedContents(outfile, fmt.Sprintf("```plantuml\n%s\n```\n", out.String()), pertRegex, pertTag)
	} else {
		outfile.WriteString(out.String())
	}
}

func WBS(sheets []Sheet, outfile *os.File, config *cfg) {
	out := bytes.NewBufferString("")

	out.WriteString("@startwbs\n")
	out.WriteString("* Project\n")
	for _, sheet := range sheets {
		out.WriteString(sheet.GetWBSLevel(config.Level))
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
		out.WriteString(sheet.MarkdownRow())
		out.WriteString("\n")
	}
	if config.Embed && config.Output != "-" {
		embedContents(outfile, fmt.Sprintf("```plantuml\n%s\n```\n", out.String()), wbsTableRegex, wbsTableTag)
	} else {
		outfile.WriteString(out.String())
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
