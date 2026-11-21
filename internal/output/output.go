package output

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiDim    = "\x1b[2m"
	ansiRed    = "\x1b[31m"
	ansiGreen  = "\x1b[32m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
)

// KV represents a single labeled field for structured key-value output.
type KV struct {
	Key   string
	Value string
}

// Printer centralizes all CLI output, styling, and TTY-aware coloring.
type Printer struct {
	out      io.Writer
	err      io.Writer
	outIsTTY bool
	outColor bool
	errColor bool
}

// New creates a Printer wired to stdout and stderr with automatic TTY/NO_COLOR detection.
func New() *Printer {
	outTTY := isTTY(os.Stdout)
	errTTY := isTTY(os.Stderr)
	allowColor := colorAllowed()
	return &Printer{
		out:      os.Stdout,
		err:      os.Stderr,
		outIsTTY: outTTY,
		outColor: outTTY && allowColor,
		errColor: errTTY && allowColor,
	}
}

// NewWithWriters creates a Printer with explicit writers and color control (useful in tests).
func NewWithWriters(out, err io.Writer, outIsTTY, color bool) *Printer {
	return &Printer{
		out:      out,
		err:      err,
		outIsTTY: outIsTTY,
		outColor: color,
		errColor: color,
	}
}

func isTTY(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

func colorAllowed() bool {
	if _, noColor := os.LookupEnv("NO_COLOR"); noColor {
		return false
	}
	if strings.EqualFold(os.Getenv("TERM"), "dumb") {
		return false
	}
	return true
}

func (p *Printer) paintOut(code, text string) string {
	if !p.outColor || text == "" {
		return text
	}
	return code + text + ansiReset
}

func (p *Printer) paintErr(code, text string) string {
	if !p.errColor || text == "" {
		return text
	}
	return code + text + ansiReset
}

// Style helpers for building values passed to KeyValues or Table.
func (p *Printer) Bold(s string) string   { return p.paintOut(ansiBold, s) }
func (p *Printer) Dim(s string) string    { return p.paintOut(ansiDim, s) }
func (p *Printer) Green(s string) string  { return p.paintOut(ansiGreen, s) }
func (p *Printer) Red(s string) string    { return p.paintOut(ansiRed, s) }
func (p *Printer) Yellow(s string) string { return p.paintOut(ansiYellow, s) }
func (p *Printer) Cyan(s string) string   { return p.paintOut(ansiCyan, s) }

// Success prints a green confirmation line to stdout.
func (p *Printer) Success(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := p.paintOut(ansiGreen+ansiBold, "✓")
	_, _ = fmt.Fprintf(p.out, "%s %s\n", prefix, msg)
}

// Info prints a neutral informational line to stdout.
func (p *Printer) Info(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintln(p.out, msg)
}

// DimInfo prints a muted informational line to stdout.
func (p *Printer) DimInfo(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	_, _ = fmt.Fprintln(p.out, p.paintOut(ansiDim, msg))
}

// Warn prints a yellow warning line to stderr.
func (p *Printer) Warn(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := p.paintErr(ansiYellow+ansiBold, "warn:")
	_, _ = fmt.Fprintf(p.err, "%s %s\n", prefix, msg)
}

// Error prints a red error line to stderr.
func (p *Printer) Error(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	prefix := p.paintErr(ansiRed+ansiBold, "error:")
	_, _ = fmt.Fprintf(p.err, "%s %s\n", prefix, msg)
}

// Prompt writes an interactive input prompt to stderr without a trailing newline.
func (p *Printer) Prompt(label string) {
	_, _ = fmt.Fprint(p.err, p.paintErr(ansiCyan, label))
}

// PromptNewline ends an interactive hidden-input prompt line on stderr.
func (p *Printer) PromptNewline() {
	_, _ = fmt.Fprintln(p.err)
}

// Raw writes a plain uncolored value to stdout, adding a newline only on a TTY.
func (p *Printer) Raw(value string) {
	if p.outIsTTY {
		_, _ = fmt.Fprintln(p.out, value)
		return
	}
	_, _ = io.WriteString(p.out, value)
}

// Line writes a plain uncolored line to stdout (useful for machine-readable values like `config get`).
func (p *Printer) Line(value string) {
	_, _ = fmt.Fprintln(p.out, value)
}

// KeyValues prints aligned label/value rows to stdout.
func (p *Printer) KeyValues(rows []KV) {
	maxKey := 0
	for _, r := range rows {
		if len(r.Key) > maxKey {
			maxKey = len(r.Key)
		}
	}
	for _, r := range rows {
		pad := strings.Repeat(" ", maxKey-len(r.Key))
		label := p.paintOut(ansiDim, r.Key+":")
		_, _ = fmt.Fprintf(p.out, "%s%s  %s\n", label, pad, r.Value)
	}
}

// Table prints an ANSI-aware aligned table with styled headers to stdout.
func (p *Printer) Table(headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = visibleWidth(h)
	}
	for _, row := range rows {
		for i := 0; i < len(row) && i < len(widths); i++ {
			if w := visibleWidth(row[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	writeRow := func(cells []string, styleHeader bool) {
		var b strings.Builder
		for i, cell := range cells {
			rendered := cell
			if styleHeader {
				rendered = p.paintOut(ansiBold+ansiDim, cell)
			}
			b.WriteString(rendered)
			if i < len(cells)-1 && i < len(widths) {
				pad := widths[i] - visibleWidth(cell) + 2
				if pad < 2 {
					pad = 2
				}
				b.WriteString(strings.Repeat(" ", pad))
			}
		}
		_, _ = fmt.Fprintln(p.out, b.String())
	}

	writeRow(headers, true)
	for _, row := range rows {
		writeRow(row, false)
	}
}

// Section writes a bold section heading to stdout.
func (p *Printer) Section(title string) {
	_, _ = fmt.Fprintln(p.out, p.paintOut(ansiBold, title))
}

func visibleWidth(s string) int {
	width := 0
	inEsc := false
	for i := 0; i < len(s); {
		if !inEsc && s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			inEsc = true
			i += 2
			continue
		}
		if inEsc {
			if s[i] == 'm' {
				inEsc = false
			}
			i++
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		width++
		i += size
	}
	return width
}
