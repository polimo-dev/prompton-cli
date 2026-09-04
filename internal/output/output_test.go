package output_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/polimo-dev/prompton-cli/internal/output"
)

func newPrinter(asJSON, quiet bool) (*output.Printer, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	return output.New(&out, &errOut, asJSON, quiet), &out, &errOut
}

func TestInfoGoesToStdoutNormally(t *testing.T) {
	p, out, errOut := newPrinter(false, false)
	p.Info("hello %s", "world")
	if got := out.String(); got != "hello world\n" {
		t.Errorf("stdout = %q", got)
	}
	if errOut.Len() != 0 {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestInfoMovesToStderrUnderJSON(t *testing.T) {
	p, out, errOut := newPrinter(true, false)
	p.Info("progress")
	if out.Len() != 0 {
		t.Errorf("stdout must stay clean for the JSON document, got %q", out.String())
	}
	if !strings.Contains(errOut.String(), "progress") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestInfoIsSilentUnderQuiet(t *testing.T) {
	p, out, errOut := newPrinter(false, true)
	p.Info("chatter")
	if out.Len() != 0 || errOut.Len() != 0 {
		t.Errorf("quiet printed something: %q / %q", out.String(), errOut.String())
	}
}

func TestWarnSurvivesQuiet(t *testing.T) {
	p, _, errOut := newPrinter(false, true)
	p.Warn("careful")
	if !strings.Contains(errOut.String(), "careful") {
		t.Errorf("stderr = %q, warnings are not chatter", errOut.String())
	}
}

func TestLineIgnoresQuiet(t *testing.T) {
	p, out, _ := newPrinter(false, true)
	p.Line("the-value")
	if got := out.String(); got != "the-value\n" {
		t.Errorf("stdout = %q", got)
	}
}

func TestPrintJSONIsIndentedAndUnescaped(t *testing.T) {
	p, out, _ := newPrinter(true, false)
	if err := p.PrintJSON(map[string]any{"uri": "https://x/y?a=b&c=d"}); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if !strings.Contains(body, "\n  ") {
		t.Errorf("output is not indented: %q", body)
	}
	if strings.Contains(body, `\u0026`) {
		t.Errorf("HTML escaping makes URLs unreadable: %q", body)
	}
	if !strings.Contains(body, "a=b&c=d") {
		t.Errorf("the URL did not survive verbatim: %q", body)
	}
	var back map[string]any
	if err := json.Unmarshal([]byte(body), &back); err != nil {
		t.Fatalf("not valid JSON: %v", err)
	}
}

func TestTablePrintsHeaderEvenWithNoRows(t *testing.T) {
	p, out, _ := newPrinter(false, false)
	p.Table([]string{"SLUG", "NAME"}, nil)
	if !strings.Contains(out.String(), "SLUG") {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestTableDropsTheHeaderUnderQuiet(t *testing.T) {
	p, out, _ := newPrinter(false, true)
	p.Table([]string{"SLUG"}, [][]string{{"helpdesk"}})
	got := out.String()
	if strings.Contains(got, "SLUG") {
		t.Errorf("quiet output should be values only, got %q", got)
	}
	if !strings.Contains(got, "helpdesk") {
		t.Errorf("stdout = %q", got)
	}
}

func TestTableKeepsColumnsIntactWhenCellsContainWhitespace(t *testing.T) {
	p, out, _ := newPrinter(false, false)
	p.Table([]string{"A", "B"}, [][]string{{"one\ttwo", "three\nfour"}})
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("a cell's newline broke the table: %q", out.String())
	}
}

func TestCompactIsStablyOrdered(t *testing.T) {
	m := map[string]any{"temperature": 0.4, "max_tokens": float64(512), "stream": true, "stop": nil}
	first := output.Compact(m)
	for i := 0; i < 20; i++ {
		if got := output.Compact(m); got != first {
			t.Fatalf("Compact is not deterministic: %q vs %q", got, first)
		}
	}
	if first != "max_tokens=512 stop=null stream=true temperature=0.4" {
		t.Errorf("Compact = %q", first)
	}
}

func TestCompactRendersNestedValuesAsJSON(t *testing.T) {
	got := output.Compact(map[string]any{"only": []any{"Anthropic"}})
	if got != `only=["Anthropic"]` {
		t.Errorf("Compact = %q", got)
	}
}

func TestCompactStringsIsStablyOrdered(t *testing.T) {
	got := output.CompactStrings(map[string]string{"ko": "v7", "default": "v2"})
	if got != "default=v2 ko=v7" {
		t.Errorf("CompactStrings = %q", got)
	}
}

func TestEmptyMapsRenderEmpty(t *testing.T) {
	if output.Compact(nil) != "" || output.CompactStrings(nil) != "" {
		t.Error("an empty map must render as nothing, so Dash can take over")
	}
}

func TestDash(t *testing.T) {
	if output.Dash("") != "-" || output.Dash("  ") != "-" {
		t.Error("blank values must become a dash")
	}
	if output.Dash("x") != "x" {
		t.Error("real values must pass through")
	}
}

func TestStrOnNil(t *testing.T) {
	if output.Str(nil) != "" {
		t.Error("a null string must render empty")
	}
	v := "value"
	if output.Str(&v) != "value" {
		t.Error("a set string must render itself")
	}
}

func TestDateTrimsToMinutes(t *testing.T) {
	if got := output.Date("2026-09-01T09:12:03.123456Z"); got != "2026-09-01 09:12Z" {
		t.Errorf("Date = %q", got)
	}
	if got := output.Date("not a date"); got != "not a date" {
		t.Errorf("Date must leave unfamiliar values alone, got %q", got)
	}
	if got := output.Date(""); got != "" {
		t.Errorf("Date = %q", got)
	}
}

func TestTruncateCountsRunes(t *testing.T) {
	// Korean, from the `ko` prompt example: Truncate counts runes, not bytes.
	if got := output.Truncate("고객지원상담원", 4); got != "고객지…" {
		t.Errorf("Truncate = %q", got)
	}
	if got := output.Truncate("short", 10); got != "short" {
		t.Errorf("Truncate = %q", got)
	}
}

func TestJoinAndBool(t *testing.T) {
	if output.Join([]string{"read", "logs"}) != "read,logs" {
		t.Error("Join")
	}
	if output.Join(nil) != "" {
		t.Error("Join(nil)")
	}
	if output.Bool(true) != "yes" || output.Bool(false) != "no" {
		t.Error("Bool")
	}
}

func TestFieldsAligns(t *testing.T) {
	p, out, _ := newPrinter(false, false)
	p.Fields([][2]string{{"Slug", "helpdesk"}, {"Timezone", "Etc/UTC"}})
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("output = %q", out.String())
	}
	if !strings.HasPrefix(lines[0], "Slug") || !strings.Contains(lines[0], "helpdesk") {
		t.Errorf("line = %q", lines[0])
	}
}
