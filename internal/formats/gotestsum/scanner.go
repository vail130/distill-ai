package gotestsum

import (
	"bufio"
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/vail130/distill-ai/internal/event"
)

var (
	failHeaderLinePattern = regexp.MustCompile(`^=== FAIL: (.+?)(?: \(([^)]+)\))?\s*$`)
	locationLinePattern   = regexp.MustCompile(`^\s*(\S+\.go):(\d+)(?::(\d+))?:\s+(.*)$`)
)

type pendingFailure struct {
	subject  string
	duration string
	body     []string
}

func parseStream(ctx context.Context, r io.Reader) <-chan event.Event {
	out := make(chan event.Event, 1)
	go func() {
		defer close(out)
		_ = scanLoop(ctx, r, out)
	}()
	return out
}

func scanLoop(ctx context.Context, r io.Reader, out chan<- event.Event) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var cur *pendingFailure
	emitted := false
	var failingSummary string
	flush := func() error {
		if cur == nil {
			return nil
		}
		ev := finaliseFailure(cur)
		cur = nil
		emitted = true
		return sendEvent(ctx, out, ev)
	}
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := sc.Text()
		if m := failHeaderLinePattern.FindStringSubmatch(line); m != nil {
			if err := flush(); err != nil {
				return err
			}
			cur = &pendingFailure{subject: strings.TrimSpace(m[1]), duration: m[2], body: []string{line}}
			continue
		}
		if strings.HasPrefix(line, "DONE ") {
			if isFailingSummary(line) {
				failingSummary = line
			}
			if err := flush(); err != nil {
				return err
			}
			continue
		}
		if cur != nil {
			cur.body = append(cur.body, line)
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}
	if !emitted && failingSummary != "" {
		return sendEvent(ctx, out, summaryEvent(failingSummary))
	}
	return nil
}

func finaliseFailure(p *pendingFailure) event.Event {
	pkg, testID := splitSubject(p.subject)
	title, loc := titleAndLocation(p)
	kind := "test_failure"
	if testID == "" && looksLikeBuildFailure(title) {
		kind = "build_failure"
	}
	meta := map[string]string{"subject": p.subject}
	if pkg != "" {
		meta["package"] = pkg
	}
	if testID != "" {
		meta["test_id"] = testID
	}
	if p.duration != "" {
		meta["duration"] = p.duration
	}
	return event.Event{
		Severity: event.SeverityError,
		Kind:     kind,
		Title:    title,
		Location: loc,
		Body:     append([]string(nil), p.body...),
		Metadata: meta,
	}
}

func splitSubject(subject string) (pkg, testID string) {
	fields := strings.Fields(subject)
	if len(fields) >= 2 {
		return fields[0], fields[1]
	}
	lastSlash := strings.LastIndex(subject, "/")
	lastDot := strings.LastIndex(subject, ".")
	if lastDot > lastSlash && lastDot > 0 && lastDot < len(subject)-1 {
		return subject[:lastDot], subject[lastDot+1:]
	}
	return subject, ""
}

func titleAndLocation(p *pendingFailure) (string, *event.Location) {
	for _, line := range p.body[1:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if m := locationLinePattern.FindStringSubmatch(line); m != nil {
			lineNo, _ := strconv.Atoi(m[2])
			loc := &event.Location{File: m[1], Line: lineNo}
			if m[3] != "" {
				col, _ := strconv.Atoi(m[3])
				loc.Column = &col
			}
			return m[4], loc
		}
		return trimmed, nil
	}
	return p.subject, nil
}

func looksLikeBuildFailure(title string) bool {
	return strings.HasPrefix(title, "flag provided but not defined:") || strings.Contains(title, "build failed")
}

func isFailingSummary(line string) bool {
	return strings.Contains(line, " failure") || strings.Contains(line, " failures") || strings.Contains(line, " error") || strings.Contains(line, " errors")
}

func summaryEvent(line string) event.Event {
	return event.Event{
		Severity: event.SeverityError,
		Kind:     "test_failure",
		Title:    line,
		Body:     []string{line},
		Metadata: map[string]string{"summary_only": "true"},
	}
}

func sendEvent(ctx context.Context, out chan<- event.Event, ev event.Event) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case out <- ev:
		return nil
	}
}
