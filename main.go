package main

import (
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"os"
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
}

type Sheet struct {
	WBS      string `csv:"Task"`
	Title    string `csv:"Title"`
	Parents  string `csv:"Parents"`
	Duration int    `csv:"Duration,omitempty"`
}

const pertNode = `
map "%s: %s" as %s {
	Duration => %d
}
`
const markDownRow = "| %s | %s | %s | %s |"

// GetParents splits the parents and returns
// them as a list of strings
func (s *Sheet) GetParents() []string {
	parents := strings.Split(s.Parents, ",")
	for i, p := range parents {
		parents[i] = strings.Trim(p, " ")
	}
	return parents
}

// GetPertNode returns a PlantUML string that represents
// the task in a PERT chart
func (s *Sheet) GetPertNode() string {
	return fmt.Sprintf(pertNode, s.WBS, s.Title, s.WBS, s.Duration)
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
	if s.GetLevel() > lvl {
		str = str + "_"
	}
	str = fmt.Sprintf("%s %s: %s", str, s.WBS, s.Title)
	return str
}

// MarkdownRow returns a markdown table row representing the task
func (s *Sheet) MarkdownRow() string {
	return fmt.Sprintf(markDownRow, s.WBS, s.Title, s.Parents, strconv.Itoa(s.Duration))
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

func buildDecoder(in *os.File) *csvutil.Decoder {
	csvReader := csv.NewReader(in)
	decoder, err := csvutil.NewDecoder(csvReader)
	if err != nil {
		log.Fatal(err)
	}
	return decoder
}

func PertChart(in *os.File, out *os.File, config *cfg) {
	decoder := buildDecoder(in)
	out.WriteString("@startuml PERT\n")
	out.WriteString("left to right direction\n")
	out.WriteString("map Start {\n}\n")
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
	out.WriteString("@enduml\n")
}

func WBS(in *os.File, out *os.File, config *cfg) {
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
	out.WriteString("@endwbs\n")
}

func WBSTable(in *os.File, out *os.File, config *cfg) {
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
