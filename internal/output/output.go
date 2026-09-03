// Package output renders command results either as human tables or as JSON.
//
// The rule that keeps --json usable from a script: in JSON mode stdout carries
// exactly one JSON document and nothing else. Progress and advice go to
// stderr, where a pipeline ignores them.
package output

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// Printer writes command output.
type Printer struct {
	Out   io.Writer
	Err   io.Writer
	JSON  bool
	Quiet bool
}

// New returns a Printer writing to out and err.
func New(out, err io.Writer, asJSON, quiet bool) *Printer {
	return &Printer{Out: out, Err: err, JSON: asJSON, Quiet: quiet}
}

// PrintJSON writes v to stdout as indented JSON.
func (p *Printer) PrintJSON(v any) error {
	enc := json.NewEncoder(p.Out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Info writes a human-facing progress line. It is silent under --quiet, and
// goes to stderr under --json so it cannot corrupt the document on stdout.
func (p *Printer) Info(format string, args ...any) {
	if p.Quiet {
		return
	}
	w := p.Out
	if p.JSON {
		w = p.Err
	}
	fmt.Fprintf(w, format+"\n", args...)
}

// Line writes one raw line to stdout, whatever the flags say. It is for the
// case where the output *is* the value, such as a freshly issued key under
// --quiet.
func (p *Printer) Line(s string) { fmt.Fprintln(p.Out, s) }

// Warn writes a warning to stderr. --quiet does not silence it; only the shell
// can.
func (p *Printer) Warn(format string, args ...any) {
	fmt.Fprintf(p.Err, format+"\n", args...)
}

// Table writes aligned columns to stdout. An empty rows slice prints the
// header only, so `list` on an empty project still shows the shape.
func (p *Printer) Table(headers []string, rows [][]string) {
	if p.Quiet && len(rows) == 0 {
		return
	}
	tw := tabwriter.NewWriter(p.Out, 0, 0, 2, ' ', 0)
	if !p.Quiet {
		fmt.Fprintln(tw, strings.Join(headers, "\t"))
	}
	for _, r := range rows {
		cells := make([]string, len(r))
		for i, c := range r {
			cells[i] = clean(c)
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	tw.Flush()
}

// Fields writes a "key: value" block for a single resource.
func (p *Printer) Fields(pairs [][2]string) {
	tw := tabwriter.NewWriter(p.Out, 0, 0, 2, ' ', 0)
	for _, kv := range pairs {
		fmt.Fprintf(tw, "%s\t%s\n", kv[0], clean(kv[1]))
	}
	tw.Flush()
}

// Dash renders an empty value as "-" so a table column never collapses.
func Dash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// Str dereferences a nullable string.
func Str(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// Bool renders a boolean as yes/no.
func Bool(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// Int renders an int, with 0 shown as "-" when zero means "unset".
func Int(n int) string { return strconv.Itoa(n) }

// Join renders a string slice as a comma-separated list.
func Join(vals []string) string {
	if len(vals) == 0 {
		return ""
	}
	return strings.Join(vals, ",")
}

// Compact renders a map as a stable, compact "k=v" list. Keys are sorted so
// two runs of the same command produce identical output.
func Compact(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, scalar(m[k])))
	}
	return strings.Join(parts, " ")
}

// CompactStrings renders a string map the same way.
func CompactStrings(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+m[k])
	}
	return strings.Join(parts, " ")
}

// Date trims an ISO8601 timestamp to minute precision for table display.
func Date(ts string) string {
	if len(ts) >= 16 && ts[10] == 'T' {
		return ts[:10] + " " + ts[11:16] + "Z"
	}
	return ts
}

// Truncate shortens s to at most n runes, ending with an ellipsis.
func Truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

func scalar(v any) string {
	switch t := v.(type) {
	case nil:
		return "null"
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		raw, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(raw)
	}
}

func clean(s string) string {
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}
