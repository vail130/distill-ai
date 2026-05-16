package event

import (
	"reflect"
	"testing"
)

func frame(file, fn string) StackFrame {
	return StackFrame{File: file, Function: fn}
}

func TestClassify_Python_SitePackages(t *testing.T) {
	cases := []string{
		"/home/u/.venv/lib/python3.11/site-packages/requests/api.py",
		"/usr/lib/python3.10/dist-packages/urllib3/connection.py",
		"site-packages/foo/bar.py",
	}
	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			if !Classify(frame(file, "")) {
				t.Errorf("Classify(%q) = false, want true", file)
			}
		})
	}
}

func TestClassify_Python_Stdlib(t *testing.T) {
	cases := []string{
		"/usr/lib/python3.11/json/decoder.py",
		"/usr/local/lib/python3.10/asyncio/base_events.py",
	}
	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			if !Classify(frame(file, "")) {
				t.Errorf("Classify(%q) = false, want true", file)
			}
		})
	}
}

func TestClassify_Python_FrozenImport(t *testing.T) {
	if !Classify(frame("<frozen importlib._bootstrap>", "")) {
		t.Error("frozen importlib not classified as vendor")
	}
}

func TestClassify_Node_Modules(t *testing.T) {
	cases := []string{
		"/app/node_modules/express/lib/router.js",
		"node_modules/.pnpm/foo@1.0/node_modules/foo/index.js",
	}
	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			if !Classify(frame(file, "")) {
				t.Errorf("Classify(%q) = false, want true", file)
			}
		})
	}
}

func TestClassify_Go_Stdlib(t *testing.T) {
	cases := []string{
		"/usr/local/go/src/runtime/proc.go",
		"/opt/homebrew/Cellar/go/1.22.0/libexec/src/runtime/panic.go",
	}
	for _, file := range cases {
		t.Run(file, func(t *testing.T) {
			if !Classify(frame(file, "")) {
				t.Errorf("Classify(%q) = false, want true", file)
			}
		})
	}
}

func TestClassify_Go_PkgMod(t *testing.T) {
	if !Classify(frame("/Users/u/go/pkg/mod/github.com/stretchr/testify@v1.8.0/assert/assertions.go", "")) {
		t.Error("pkg/mod path not classified as vendor")
	}
}

func TestClassify_Go_Vendor(t *testing.T) {
	if !Classify(frame("/app/vendor/github.com/foo/bar/x.go", "")) {
		t.Error("vendor/ path not classified as vendor")
	}
}

func TestClassify_JVM_JavaPrefix(t *testing.T) {
	cases := []string{
		"java.util.ArrayList$Itr.next",
		"javax.servlet.http.HttpServlet.service",
		"sun.reflect.NativeMethodAccessorImpl.invoke",
		"jdk.internal.reflect.GeneratedMethodAccessor.invoke",
	}
	for _, fn := range cases {
		t.Run(fn, func(t *testing.T) {
			if !Classify(frame("Anywhere.java", fn)) {
				t.Errorf("Classify(function=%q) = false, want true", fn)
			}
		})
	}
}

func TestClassify_JVM_TestFramework(t *testing.T) {
	cases := []string{
		"org.junit.runners.ParentRunner.run",
		"org.gradle.api.internal.tasks.testing.SuiteTestClassProcessor",
	}
	for _, fn := range cases {
		t.Run(fn, func(t *testing.T) {
			if !Classify(frame("Anywhere.java", fn)) {
				t.Errorf("Classify(function=%q) = false, want true", fn)
			}
		})
	}
}

func TestClassify_UserCode_NotVendor(t *testing.T) {
	cases := []StackFrame{
		{File: "internal/api/handler.go", Function: "Handler.ServeHTTP"},
		{File: "app/views.py", Function: "login_view"},
		{File: "src/controllers/login.ts", Function: "LoginController.post"},
		{File: "src/main/java/com/myapp/Login.java", Function: "com.myapp.Login.handle"},
	}
	for _, f := range cases {
		t.Run(f.File, func(t *testing.T) {
			if Classify(f) {
				t.Errorf("Classify(%+v) = true, want false", f)
			}
		})
	}
}

func TestClassifyFrames_DoesNotMutateInput(t *testing.T) {
	in := []StackFrame{
		{File: "app/main.py", Vendor: true},                      // wrong hint
		{File: "/lib/python3/site-packages/x.py", Vendor: false}, // wrong hint
	}
	original := append([]StackFrame(nil), in...)
	out := ClassifyFrames(in)
	if !reflect.DeepEqual(in, original) {
		t.Errorf("ClassifyFrames mutated input: got %+v, want %+v", in, original)
	}
	if out[0].Vendor {
		t.Errorf("user frame mis-classified as vendor")
	}
	if !out[1].Vendor {
		t.Errorf("site-packages frame not classified as vendor")
	}
}

func TestClassifyFrames_Empty(t *testing.T) {
	if got := ClassifyFrames(nil); got != nil {
		t.Errorf("ClassifyFrames(nil) = %+v, want nil", got)
	}
}

func TestCollapse_MiddleVendorRun(t *testing.T) {
	in := []StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3/site-packages/requests/api.py"},
		{File: "/lib/python3/site-packages/requests/sessions.py"},
		{File: "app/handler.py"},
	}
	out, collapsed := Collapse(in, false)
	if collapsed != 2 {
		t.Errorf("collapsed=%d, want 2", collapsed)
	}
	if len(out) != 2 {
		t.Fatalf("len(out)=%d, want 2: %+v", len(out), out)
	}
	if out[0].File != "app/main.py" || out[1].File != "app/handler.py" {
		t.Errorf("collapsed result = %+v", out)
	}
}

func TestCollapse_LeadingTrailingVendorRuns(t *testing.T) {
	in := []StackFrame{
		{File: "/lib/python3/site-packages/a.py"},
		{File: "/lib/python3/site-packages/b.py"},
		{File: "app/user.py"},
		{File: "node_modules/x.js"},
		{File: "node_modules/y.js"},
	}
	out, collapsed := Collapse(in, false)
	if collapsed != 4 {
		t.Errorf("collapsed=%d, want 4", collapsed)
	}
	if len(out) != 1 || out[0].File != "app/user.py" {
		t.Errorf("out = %+v, want one user.py frame", out)
	}
}

func TestCollapse_AllVendor(t *testing.T) {
	in := []StackFrame{
		{File: "/lib/python3/site-packages/a.py"},
		{File: "/lib/python3/site-packages/b.py"},
	}
	out, collapsed := Collapse(in, false)
	if collapsed != 2 {
		t.Errorf("collapsed=%d, want 2", collapsed)
	}
	if len(out) != 0 {
		t.Errorf("out=%+v, want empty", out)
	}
}

func TestCollapse_AllUser(t *testing.T) {
	in := []StackFrame{
		{File: "app/a.py"},
		{File: "app/b.py"},
	}
	out, collapsed := Collapse(in, false)
	if collapsed != 0 {
		t.Errorf("collapsed=%d, want 0", collapsed)
	}
	if len(out) != 2 {
		t.Fatalf("len(out)=%d, want 2", len(out))
	}
	for _, f := range out {
		if f.Vendor {
			t.Errorf("user frame %+v marked Vendor", f)
		}
	}
}

func TestCollapse_KeepVendor(t *testing.T) {
	in := []StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3/site-packages/requests/api.py"},
		{File: "app/handler.py"},
	}
	out, collapsed := Collapse(in, true)
	if collapsed != 0 {
		t.Errorf("keepVendor=true: collapsed=%d, want 0", collapsed)
	}
	if len(out) != 3 {
		t.Fatalf("keepVendor=true: len(out)=%d, want 3", len(out))
	}
	wantVendor := []bool{false, true, false}
	for i, want := range wantVendor {
		if out[i].Vendor != want {
			t.Errorf("frame[%d].Vendor=%v, want %v", i, out[i].Vendor, want)
		}
	}
}

func TestCollapse_EmptyInput(t *testing.T) {
	out, collapsed := Collapse(nil, false)
	if out != nil {
		t.Errorf("out=%+v, want nil", out)
	}
	if collapsed != 0 {
		t.Errorf("collapsed=%d, want 0", collapsed)
	}
}

func TestCollapse_DoesNotMutateInput(t *testing.T) {
	in := []StackFrame{
		{File: "app/main.py"},
		{File: "/lib/python3/site-packages/x.py"},
	}
	original := append([]StackFrame(nil), in...)
	_, _ = Collapse(in, false)
	if !reflect.DeepEqual(in, original) {
		t.Errorf("Collapse mutated input: got %+v, want %+v", in, original)
	}
}
