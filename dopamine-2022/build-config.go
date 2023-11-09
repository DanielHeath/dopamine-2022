// usr/bin/env go run $0 $@; exit $?
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"text/template"
)

type Mode struct {
	Name     string
	Shortcut string
	Path     string
	Position uint64
	Body     bytes.Buffer
}

type Modes []*Mode

type Cfg struct {
	Workspaces   []string
	ScreenMap    map[string]string // logical name -> output name
	ExitModeKeys []string
	Modes        map[string]Modes
	currentMode  *Mode
	output       io.Writer
}

// Lets templates set their modename
func (c *Cfg) ModeName(n string) string {
	c.currentMode.Name = n
	return ""
}

func (c *Cfg) Workspace(name string, screen string) string {
	if _, ok := c.ScreenMap[screen]; ok {
		if screen == "main" {
			return name
		}
		return fmt.Sprintf("\"%s-%s\"", name, screen[:1])
	}

	die(fmt.Errorf("Invalid workspace (expected a key of %+v, got %s)", c.ScreenMap, screen))
	return ""
}

func (m Mode) BoundSymbols() string {
	bindmatcher := regexp.MustCompile("bindsym ([[:alpha:]]) ")
	str := m.Body.String()
	matches := bindmatcher.FindAllStringSubmatch(str, -1)
	if matches != nil {
		suggestions := []string{}
		for i := range matches {
			suggestions = append(suggestions, matches[i][1])
		}
		return "(" + strings.Join(suggestions, " ") + ")"
	}
	return ""
}

func main() {
	cfg := Cfg{Modes: make(map[string]Modes)}
	cfg.Workspaces = readlines("cfg/workspaces")
	f, err := os.Open("cfg/screens.json")
	die(err)
	die(json.NewDecoder(f).Decode(&cfg.ScreenMap))
	cfg.ExitModeKeys = readlines("cfg/exit-mode-keys")
	cfg.output = os.Stdout

	modefiles, err := filepath.Glob("cfg/modes.d/*")
	die(err)
	conffiles, err := filepath.Glob("cfg/*.conf")
	die(err)

	tmpl := template.Must(template.ParseFiles(conffiles...))
	tmpl = template.Must(tmpl.ParseFiles(modefiles...))

	fmt.Fprintf(cfg.output, "bindsym Mod4+Return exec gnome-terminal\n")

	fmt.Fprintf(cfg.output, "# Bind workspaces to screens\n")
	for _, workspace := range cfg.Workspaces {
		for logicalname, output := range cfg.ScreenMap {
			fmt.Fprintf(cfg.output, "workspace %s output %s\n", cfg.Workspace(workspace, logicalname), output)
		}
	}

	for _, path := range conffiles {
		die(tmpl.ExecuteTemplate(os.Stdout, filepath.Base(path), &cfg))
	}

	for _, path := range modefiles {
		basename := filepath.Base(path)

		shortcut, after, found := strings.Cut(basename, ".")
		name := shortcut
		var afterIdx uint64 = 0
		if found {
			name = basename
			var err error
			afterIdx, err = strconv.ParseUint(after, 10, 64)
			die(err)
		}

		mode := Mode{
			Name:     name,
			Shortcut: shortcut,
			Path:     path,
			Position: afterIdx,
			Body:     bytes.Buffer{},
		}
		cfg.Modes[shortcut] = append(cfg.Modes[shortcut], &mode)

		cfg.currentMode = &mode
		die(tmpl.ExecuteTemplate(&mode.Body, basename, &cfg))
	}

	for shortcut, modes := range cfg.Modes {
		for _, mode := range modes {
			bs := mode.BoundSymbols()
			if bs != "" {
				mode.Name = mode.Name + " " + bs
			}
		}
		sort.Slice(modes, func(i, j int) bool { return modes[i].Position > modes[j].Position })
		fmt.Fprintf(cfg.output, "bindsym %s mode \"%s\"\n", shortcut, modes[0].Name)

		for idx, mode := range modes {
			fmt.Fprintf(cfg.output, "mode \"%s\" {\n", mode.Name)

			outputWas := cfg.output
			cfg.output = newIndentWriter(cfg.output, "    ")
			fmt.Fprintf(cfg.output, "%s\n", string(mode.Body.Bytes()))

			if len(modes) > 1 {
				// Multi-mode toggles
				fmt.Fprintf(cfg.output, "bindsym %s mode \"%s\"\n", mode.Shortcut, modes[((idx+1)%len(modes))].Name)
			} else {
				fmt.Fprintf(cfg.output, "bindsym %s exec \"~/.config/i3/dopamine-2022/passthrukey F1 %s\"\n", mode.Shortcut, mode.Shortcut)
			}

			for _, key := range cfg.ExitModeKeys {
				fmt.Fprintf(cfg.output, "bindsym %s mode default\n", key)
			}
			cfg.output = outputWas
			fmt.Fprintf(cfg.output, "}\n\n")
		}
	}

}

func die(err error) {
	if err != nil {
		panic(err)
	}
}

func readlines(path string) []string {
	f, err := os.Open(path)
	die(err)
	contents, err := io.ReadAll(f)
	die(err)

	return strings.Split(strings.Trim(string(contents), "\n"), "\n")
}

type indentWriter struct {
	w      io.Writer
	prefix []byte

	wroteIndent bool
}

// New creates a new indent writer that indents non-empty lines with indent number of tabs.
func newIndentWriter(w io.Writer, indent string) io.Writer {
	return &indentWriter{
		w:      w,
		prefix: []byte(indent),
	}
}

func (iw *indentWriter) Write(p []byte) (n int, err error) {
	for i, b := range p {
		if b == '\n' {
			iw.wroteIndent = false
		} else {
			if !iw.wroteIndent {
				_, err = iw.w.Write(iw.prefix)
				if err != nil {
					return n, err
				}
				iw.wroteIndent = true
			}
		}
		_, err = iw.w.Write(p[i : i+1])
		if err != nil {
			return n, err
		}
		n++
	}
	return len(p), nil
}
