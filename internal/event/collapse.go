package event

import (
	"regexp"
)

// VendorPattern identifies a class of stack frames as third-party /
// runtime / library code that can be collapsed when --keep-vendor is
// false. A pattern matches either against StackFrame.File (path-style
// matching) or StackFrame.Function (symbol-style matching), depending
// on Target.
//
// Patterns are compiled once at package init; runtime classification
// is O(frames × patterns), constant time per frame.
type VendorPattern struct {
	// Label is the human-readable name of what this pattern catches
	// ("Python site-packages", "Go runtime", "JVM java.* package").
	// Used only in diagnostics; not part of any output schema.
	Label string

	// Target selects which StackFrame field the pattern matches
	// against.
	Target VendorTarget

	// Regex is the compiled matcher. Must not be nil.
	Regex *regexp.Regexp
}

// VendorTarget identifies which StackFrame field a VendorPattern
// inspects.
type VendorTarget int

const (
	// VendorTargetFile matches against StackFrame.File. Use for
	// path-based detection (site-packages, node_modules, vendor/).
	VendorTargetFile VendorTarget = iota

	// VendorTargetFunction matches against StackFrame.Function. Use
	// for symbol-based detection (java.*, runtime.*).
	VendorTargetFunction
)

// DefaultPatterns is the package-level catalogue of vendor-frame
// signatures Classify consults. The catalogue covers the languages
// distill-ai's v1 formats target: Python (pytest), JavaScript (jest),
// Go (gotest), and JVM stacks that appear in generic fallback runs.
//
// Adding a language: append patterns here in the same commit as the
// format that needs them. New patterns must come with a unit test in
// collapse_test.go demonstrating a true-positive and a true-negative.
//
// See docs/formats/vendor-frames.md for the human-readable catalogue.
var DefaultPatterns = []VendorPattern{
	// Python: pip-installed packages and stdlib.
	{
		Label:  "Python site-packages",
		Target: VendorTargetFile,
		Regex:  regexp.MustCompile(`(?:^|/)(?:site|dist)-packages/`),
	},
	{
		Label:  "Python stdlib",
		Target: VendorTargetFile,
		Regex:  regexp.MustCompile(`(?:^|/)(?:usr/lib|usr/local/lib|opt/.+?/lib)/python\d+(?:\.\d+)?/`),
	},
	{
		Label:  "Python frozen importlib",
		Target: VendorTargetFile,
		Regex:  regexp.MustCompile(`^<frozen `),
	},
	// Node: anything under node_modules.
	{
		Label:  "Node modules",
		Target: VendorTargetFile,
		Regex:  regexp.MustCompile(`(?:^|/)node_modules/`),
	},
	// Go: stdlib runtime/, vendored code, module cache.
	{
		Label:  "Go runtime",
		Target: VendorTargetFile,
		Regex:  regexp.MustCompile(`(?:^|/)src/runtime/`),
	},
	{
		Label:  "Go vendor",
		Target: VendorTargetFile,
		Regex:  regexp.MustCompile(`(?:^|/)vendor/`),
	},
	{
		Label:  "Go module cache",
		Target: VendorTargetFile,
		Regex:  regexp.MustCompile(`(?:^|/)pkg/mod/`),
	},
	// JVM: java.*/javax.*/sun.*/jdk.* runtime packages and the
	// common test/build framework packages.
	{
		Label:  "JVM runtime",
		Target: VendorTargetFunction,
		Regex:  regexp.MustCompile(`^(?:java|javax|sun|jdk)\.`),
	},
	{
		Label:  "JVM test framework",
		Target: VendorTargetFunction,
		Regex:  regexp.MustCompile(`^(?:org\.junit|org\.gradle|org\.testng)\.`),
	},
}

// Classify reports whether frame matches any pattern in
// DefaultPatterns. It is a pure function; no global mutable state.
func Classify(frame StackFrame) bool {
	for i := range DefaultPatterns {
		p := &DefaultPatterns[i]
		var target string
		switch p.Target {
		case VendorTargetFile:
			target = frame.File
		case VendorTargetFunction:
			target = frame.Function
		}
		if target == "" {
			continue
		}
		if p.Regex.MatchString(target) {
			return true
		}
	}
	return false
}

// ClassifyFrames returns a copy of frames with the Vendor field set
// on every frame according to Classify. Does not mutate the input.
//
// The collapse stage is the single source of truth for vendor
// classification: format parsers may set StackFrame.Vendor as a hint
// but ClassifyFrames overwrites it, so per-format regex tables don't
// have to stay in sync with DefaultPatterns.
func ClassifyFrames(frames []StackFrame) []StackFrame {
	if len(frames) == 0 {
		return frames
	}
	out := make([]StackFrame, len(frames))
	copy(out, frames)
	for i := range out {
		out[i].Vendor = Classify(out[i])
	}
	return out
}

// Collapse returns frames with contiguous runs of vendor frames
// removed (or, with keepVendor=true, the input re-classified but
// otherwise unchanged) and the count of frames omitted. The returned
// slice is always a fresh allocation; the input is never mutated.
//
// Edge cases:
//   - Empty input: returns nil, 0.
//   - All-vendor stack: returns an empty slice, collapsed=len(input).
//   - All-user stack: returns the re-classified input as a copy,
//     collapsed=0.
//   - keepVendor=true: returns ClassifyFrames(frames), collapsed=0.
//     Vendor frames stay in place; the downstream encoder may still
//     style them differently using the Vendor bool.
func Collapse(frames []StackFrame, keepVendor bool) (out []StackFrame, collapsed int) {
	if len(frames) == 0 {
		return nil, 0
	}
	classified := ClassifyFrames(frames)
	if keepVendor {
		return classified, 0
	}
	out = make([]StackFrame, 0, len(classified))
	for _, frame := range classified {
		if frame.Vendor {
			collapsed++
			continue
		}
		out = append(out, frame)
	}
	return out, collapsed
}
