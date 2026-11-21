package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrinterPlainAndColoredOutput(t *testing.T) {
	var out, errBuf bytes.Buffer
	plain := NewWithWriters(&out, &errBuf, false, false)

	plain.Success("Unlocked (%s)", "ttl")
	plain.KeyValues([]KV{
		{Key: "Status", Value: "unlocked"},
		{Key: "Mode", Value: "ttl"},
	})
	plain.Error("something broke")

	if strings.Contains(out.String(), "\x1b[") || strings.Contains(errBuf.String(), "\x1b[") {
		t.Fatalf("expected no ANSI codes when color=false, got stdout=%q stderr=%q", out.String(), errBuf.String())
	}
	if !strings.Contains(out.String(), "✓ Unlocked (ttl)\n") {
		t.Fatalf("unexpected success output: %q", out.String())
	}
	if !strings.Contains(out.String(), "Status:  unlocked\nMode:    ttl\n") {
		t.Fatalf("unexpected key-values alignment: %q", out.String())
	}
	if errBuf.String() != "error: something broke\n" {
		t.Fatalf("unexpected error output: %q", errBuf.String())
	}

	out.Reset()
	errBuf.Reset()
	colored := NewWithWriters(&out, &errBuf, true, true)
	colored.Success("Stored %s", "API_KEY")
	colored.Error("vault locked")

	if !strings.Contains(out.String(), ansiGreen) {
		t.Fatalf("expected green ANSI escape in colored success output: %q", out.String())
	}
	if !strings.Contains(errBuf.String(), ansiRed) {
		t.Fatalf("expected red ANSI escape in colored error output: %q", errBuf.String())
	}
}

func TestPrinterRawRespectsTTY(t *testing.T) {
	var piped bytes.Buffer
	NewWithWriters(&piped, &bytes.Buffer{}, false, false).Raw("sk-secret")
	if piped.String() != "sk-secret" {
		t.Fatalf("expected raw piped value without newline, got %q", piped.String())
	}

	var tty bytes.Buffer
	NewWithWriters(&tty, &bytes.Buffer{}, true, true).Raw("sk-secret")
	if tty.String() != "sk-secret\n" {
		t.Fatalf("expected TTY value with newline and no ANSI codes, got %q", tty.String())
	}
}

func TestTableAlignsWithANSIColors(t *testing.T) {
	var buf bytes.Buffer
	p := NewWithWriters(&buf, &bytes.Buffer{}, true, true)
	p.Table(
		[]string{"KEY", "DESC"},
		[][]string{
			{p.Cyan("API_KEY"), "OpenRouter"},
			{p.Cyan("DB"), "Local PG"},
		},
	)
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d: %q", len(lines), buf.String())
	}
	// Visible column start of DESC / OpenRouter / Local PG must be identical.
	stripANSI := func(s string) string {
		var b strings.Builder
		inEsc := false
		for i := 0; i < len(s); i++ {
			if !inEsc && s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
				inEsc = true
				i++
				continue
			}
			if inEsc {
				if s[i] == 'm' {
					inEsc = false
				}
				continue
			}
			b.WriteByte(s[i])
		}
		return b.String()
	}
	col0 := strings.Index(stripANSI(lines[0]), "DESC")
	col1 := strings.Index(stripANSI(lines[1]), "OpenRouter")
	col2 := strings.Index(stripANSI(lines[2]), "Local PG")
	if col0 != col1 || col1 != col2 {
		t.Fatalf("misaligned columns: header=%d row1=%d row2=%d\n%s", col0, col1, col2, buf.String())
	}
}
