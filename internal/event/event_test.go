package event

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSeverity_String(t *testing.T) {
	cases := []struct {
		in   Severity
		want string
	}{
		{SeverityError, "error"},
		{SeverityWarn, "warn"},
		{SeverityInfo, "info"},
	}
	for _, c := range cases {
		t.Run(string(c.in), func(t *testing.T) {
			if got := c.in.String(); got != c.want {
				t.Errorf("Severity(%q).String() = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestParseSeverity(t *testing.T) {
	known := []Severity{SeverityError, SeverityWarn, SeverityInfo}
	for _, s := range known {
		t.Run("known/"+string(s), func(t *testing.T) {
			got, err := ParseSeverity(string(s))
			if err != nil {
				t.Fatalf("ParseSeverity(%q) returned error: %v", s, err)
			}
			if got != s {
				t.Errorf("ParseSeverity(%q) = %q, want %q", s, got, s)
			}
		})
	}
	unknown := []string{"", "ERROR", "fatal", "trace", "Error"}
	for _, s := range unknown {
		t.Run("unknown/"+s, func(t *testing.T) {
			got, err := ParseSeverity(s)
			if err == nil {
				t.Errorf("ParseSeverity(%q) = %q, nil; want error", s, got)
			}
		})
	}
}

func TestEvent_JSONRoundTrip(t *testing.T) {
	col := 12
	original := Event{
		Severity: SeverityError,
		Kind:     "test_failure",
		Title:    "AssertionError: expected 302, got 200",
		Location: &Location{
			File:   "tests/api/test_auth.py",
			Line:   47,
			Column: &col,
		},
		Body: []string{
			"AssertionError: expected 302, got 200",
			"  at test_auth.py:47",
		},
		Context: []string{
			`    response = client.post("/login", data=creds)`,
			"    assert response.status_code == 302",
			`>   assert response.headers["location"] == "/dashboard"`,
		},
		Frames: []StackFrame{
			{
				File:     "tests/api/test_auth.py",
				Line:     47,
				Function: "test_login_redirect",
				Vendor:   false,
			},
		},
		FramesCollapsed: 8,
		Count:           1,
		Truncated:       false,
		Metadata: map[string]string{
			"test_id": "tests/api/test_auth.py::test_login_redirect",
		},
		Raw: "this should not appear in JSON",
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if strings.Contains(string(data), "this should not appear in JSON") {
		t.Error("Event.Raw leaked into JSON output; must be json:\"-\"")
	}
	var got Event
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	original.Raw = ""
	if !reflect.DeepEqual(got, original) {
		t.Errorf("round-trip mismatch:\n got=%#v\nwant=%#v", got, original)
	}
}

func TestLocation_OptionalColumn(t *testing.T) {
	loc := Location{File: "x.go", Line: 10}
	data, err := json.Marshal(loc)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !strings.Contains(string(data), `"column":null`) {
		t.Errorf("zero-value Column should marshal as null per SCHEMA; got %s", data)
	}
}

func TestStackFrame_VendorBool(t *testing.T) {
	for _, vendor := range []bool{true, false} {
		f := StackFrame{File: "a.go", Line: 1, Vendor: vendor}
		data, err := json.Marshal(f)
		if err != nil {
			t.Fatalf("Marshal: %v", err)
		}
		var got StackFrame
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("Unmarshal: %v", err)
		}
		if got.Vendor != vendor {
			t.Errorf("vendor=%v round-trip lost: got %v", vendor, got.Vendor)
		}
	}
}

// TestEvent_JSONSchemaMatchesDoc verifies that every JSON tag in the
// Event, Location, and StackFrame structs is documented in
// docs/formats/SCHEMA.md. Catches struct/doc drift at build time.
//
// Implementation: scan the SCHEMA.md file for backticked field names
// in the table rows, then check each struct tag appears there.
func TestEvent_JSONSchemaMatchesDoc(t *testing.T) {
	schemaPath := findSchemaDoc(t)
	docBytes, err := os.ReadFile(schemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", schemaPath, err)
	}
	doc := string(docBytes)
	checkStructTagsInDoc(t, "Event", reflect.TypeOf(Event{}), doc, "")
	checkStructTagsInDoc(t, "Location", reflect.TypeOf(Location{}), doc, "location.")
	checkStructTagsInDoc(t, "StackFrame", reflect.TypeOf(StackFrame{}), doc, "frames[].")
}

func checkStructTagsInDoc(t *testing.T, typeName string, ty reflect.Type, doc, prefix string) {
	t.Helper()
	for i := 0; i < ty.NumField(); i++ {
		f := ty.Field(i)
		tag := f.Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name := strings.SplitN(tag, ",", 2)[0]
		// SCHEMA backticks fields: `name` or `prefix.name` or `prefix[].name`.
		needle := "`" + prefix + name + "`"
		if !strings.Contains(doc, needle) {
			t.Errorf("%s.%s: JSON tag %q (looking for %s) not found in SCHEMA.md; struct and doc have drifted",
				typeName, f.Name, name, needle)
		}
	}
}

// findSchemaDoc locates docs/formats/SCHEMA.md by walking up from the
// test file's directory until the repo root is found.
func findSchemaDoc(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 8; i++ {
		candidate := filepath.Join(dir, "docs", "formats", "SCHEMA.md")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate docs/formats/SCHEMA.md from working dir")
	return ""
}

func TestConfidenceThreshold(t *testing.T) {
	if ConfidenceMinDetect < 0 || ConfidenceMinDetect > 1 {
		t.Errorf("ConfidenceMinDetect must be in [0,1]; got %v", ConfidenceMinDetect)
	}
}
