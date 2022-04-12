package main

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strconv"

	"github.com/jessevdk/go-flags"
	"gopkg.in/yaml.v3"
)

type cfg struct {
	Projects []struct {
		Name     string
		Output   string
		Options  string
		Level    int
		Kanban   bool
		WBS      bool
		WBSTable bool
		PERT     bool
		Column   string
	}
}

type pluginOpts struct {
	ConfigFile string `short:"f" env:"PLUGIN_CONFIG_FILE"`
	Token      string `short:"t" long:"token" env:"PLUGIN_GITHUB_TOKEN"`
	Org        string `short:"o" long:"org" env:"PLUGIN_ORG" default:"ringsq"`
}

func main() {
	opts := &pluginOpts{}
	parser := flags.NewParser(opts, flags.Default)
	_, err := parser.Parse()
	if err != nil {
		if flgErr, ok := err.(*flags.Error); ok {
			if flgErr.Type == flags.ErrHelp {
				os.Exit(1)
			}
		}
		fmt.Println(err.Error())
		os.Exit(2)
	}
	yamlBytes, err := os.ReadFile(opts.ConfigFile)
	if err != nil {
		log.Fatal(err)
	}
	if len(opts.Token) > 0 {
		log.Printf("GITHUB_TOKEN has been set")
	} else {
		log.Fatal("GITHUB_TOKEN NOT set")
	}
	config := &cfg{}
	err = yaml.Unmarshal(yamlBytes, config)
	if err != nil {
		log.Fatal(err)
	}
	for _, project := range config.Projects {
		var args []string
		args = append(args, "--github-token", opts.Token, "--org", opts.Org, "-e", "-i", "gh", "-j", project.Name, "-o", project.Output)
		// fmt.Printf("wbsperf -i gh --github-token %s -e -j %s %s -o %s\n", opts.Token, project.Name, project.Options, project.Output)
		if project.Column != "" {
			args = append(args, "-c", project.Column)
		}
		if project.Kanban {
			args = append(args, "-k")
		}
		if project.WBS {
			args = append(args, "-w")
		}
		if project.WBSTable {
			args = append(args, "-t")
		}
		if project.PERT {
			args = append(args, "-p")
		}
		if project.Level > 0 {
			args = append(args, "-l", strconv.Itoa(project.Level))
		}
		cmd := exec.Command("wbspert", args...)
		fmt.Println(cmd)
		buf := bytes.NewBufferString("")
		cmd.Stderr = buf
		cmd.Stdout = buf
		cmd.Env = append(cmd.Env, fmt.Sprintf("GITHUB_TOKEN=%s", opts.Token))
		if err := cmd.Run(); err != nil {
			log.Println("Error runing wbspert ", err)
			log.Fatal(buf.String())
		}
	}
}
