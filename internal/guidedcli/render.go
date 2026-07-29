package guidedcli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Renderer struct {
	Out     io.Writer
	Err     io.Writer
	Color   bool
	Unicode bool
}

func NewRenderer(out, errOut io.Writer, noColor bool) Renderer {
	terminal := writerIsTerminal(out)
	color := terminal && !noColor && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	unicode := terminal && os.Getenv("FDIF_PLAIN") == "" && os.Getenv("TERM") != "dumb"
	return Renderer{Out: out, Err: errOut, Color: color, Unicode: unicode}
}

func writerIsTerminal(out io.Writer) bool {
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (r Renderer) title(text string) {
	if r.Color {
		fmt.Fprintf(r.Out, "\033[1m%s\033[0m\n", text)
		return
	}
	fmt.Fprintln(r.Out, text)
}

func (r Renderer) success(text string) {
	prefix := "OK"
	if r.Unicode {
		prefix = "✓"
	}
	if r.Color {
		fmt.Fprintf(r.Out, "\033[32m%s\033[0m %s\n", prefix, text)
		return
	}
	fmt.Fprintf(r.Out, "%s %s\n", prefix, text)
}

func (r Renderer) warning(text string) {
	prefix := "!"
	if r.Color {
		fmt.Fprintf(r.Out, "\033[33m%s %s\033[0m\n", prefix, text)
		return
	}
	fmt.Fprintf(r.Out, "%s %s\n", prefix, text)
}

func (r Renderer) next(command string) {
	arrow := "->"
	if r.Unicode {
		arrow = "→"
	}
	fmt.Fprintf(r.Out, "\nNext %s %s\n", arrow, command)
}

func (r Renderer) fields(fields ...[2]string) {
	width := 0
	for _, field := range fields {
		if len(field[0]) > width {
			width = len(field[0])
		}
	}
	for _, field := range fields {
		if strings.TrimSpace(field[1]) == "" {
			continue
		}
		fmt.Fprintf(r.Out, "  %-*s  %s\n", width, field[0], field[1])
	}
}

func writeJSON(out io.Writer, value any) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(value)
}
