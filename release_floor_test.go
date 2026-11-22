package limen_test

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// floorTool is the tool the floor is tried on: one every repository is
// required to declare, in the shared tools module.
const (
	floorModule = "github.com/forkcloser/godolint"
	floorTool   = floorModule + "/cmd/godolint"
	// floorBelow is older than any version the tools module will ever pin.
	floorBelow = "v0.0.1"
	toolsGoMod = ".limen/tools/go.mod"
)

// report is one line of `limen check -json` or `limen fix -json`.
type report struct {
	Rule    string `json:"rule"`
	Status  string `json:"status"`
	Action  string `json:"action"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// TestReleaseBuildHoldsToolFloor: a limen built as the release builds it (the
// before hook that copies the tools modules, the linker stamp, both read from
// .release-go.yaml by the pinned goreleaser) fails a repository whose tools
// module declares a tool below this limen's own, naming the file, the tool
// and both versions, and its fix raises the tool to the pin. A development
// build of the same tree, made after the release build left its copies
// behind, judges no floor. The pins exist only in a release build, so nothing
// short of one shows that the command hands them to the rules.
func TestReleaseBuildHoldsToolFloor(t *testing.T) {
	t.Parallel()
	requireUnix(t)

	own, err := os.ReadFile(toolsGoMod)
	if err != nil {
		t.Fatal(err)
	}

	pinned := regexp.MustCompile(`(?m)^(\s*` + regexp.QuoteMeta(floorModule) + ` )(v\S+)`)

	match := pinned.FindStringSubmatch(string(own))
	if match == nil {
		t.Fatalf("%s requires no %s: the test has lost its tool", toolsGoMod, floorModule)
	}

	pin := match[2]

	src := sourceCopy(t)
	dev := filepath.Join(t.TempDir(), "limen-dev")

	run(t, src, nil, "goreleaser", "build", "--config", ".release-go.yaml", "--snapshot", "--single-target")
	run(t, src, nil, "go", "build", "-o", dev, "./cmd/limen")

	built, err := filepath.Glob(filepath.Join(src, ".build", "release", "*", "limen"))
	if err != nil || len(built) != 1 {
		t.Fatalf("the release build should leave one limen under .build/release, found %v (%v)", built, err)
	}

	released := built[0]

	repo := t.TempDir()
	if err = os.MkdirAll(filepath.Join(repo, filepath.Dir(toolsGoMod)), 0o700); err != nil {
		t.Fatal(err)
	}

	behind := pinned.ReplaceAllString(string(own), "${1}"+floorBelow)
	if err = os.WriteFile(filepath.Join(repo, toolsGoMod), []byte(behind), 0o600); err != nil {
		t.Fatal(err)
	}

	finding := gotools(t, released, nil, "check", repo)
	if finding.Status != "fail" || finding.Path != toolsGoMod {
		t.Fatalf("a release build should fail a tool below its own on %s, got: %+v", toolsGoMod, finding)
	}

	for _, want := range []string{floorTool, floorBelow, pin} {
		if !strings.Contains(finding.Message, want) {
			t.Errorf("the finding does not say %q: %s", want, finding.Message)
		}
	}

	if finding := gotools(t, dev, nil, "check", repo); finding.Status != "ok" {
		t.Errorf("a development build judged a floor: %+v", finding)
	}

	stubs, asked := fakeTools(t)

	outcome := gotools(t, released, stubs, "fix", repo)
	if outcome.Action != "merged" ||
		!strings.Contains(outcome.Message, "raised to this limen's pins: "+floorTool+"@"+pin) {
		t.Fatalf("a release build's fix should raise the tool to the pin, got: %+v", outcome)
	}

	calls, err := os.ReadFile(asked)
	if err != nil {
		t.Fatal(err)
	}

	if want := "get -tool " + floorTool + "@" + pin; !strings.Contains(string(calls), want) {
		t.Errorf("fix did not run go %s; it ran:\n%s", want, calls)
	}
}

// sourceCopy is this working tree as git sees it (tracked and untracked,
// nothing ignored), committed in a repository of its own: goreleaser wants
// one, cleans its output directory and runs a hook that writes into the
// tree, none of which is done to the checkout the suite runs in.
func sourceCopy(t *testing.T) string {
	t.Helper()

	listed, err := command(t, ".", nil, "git", "ls-files", "-z",
		"--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}

	dir := t.TempDir()

	for name := range strings.SplitSeq(strings.TrimRight(string(listed), "\x00"), "\x00") {
		copyFile(t, name, filepath.Join(dir, name))
	}

	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"add", "-A"},
		{"commit", "-q", "-m", "limen, as the suite found it"},
	} {
		run(t, dir, nil, "git", args...)
	}

	return dir
}

// copyFile copies one file of the working tree with its mode; a file git
// still lists and the tree no longer has is not copied.
func copyFile(t *testing.T, from, to string) {
	t.Helper()

	info, err := os.Stat(from)
	if errors.Is(err, os.ErrNotExist) {
		return
	}

	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(to, data, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
}

// fakeTools puts a go and a hermetic that reach nothing first on PATH, as
// fix resolves both there: the go records its arguments, one call a line,
// in the file whose path is returned with the environment.
func fakeTools(t *testing.T) (env []string, asked string) {
	t.Helper()

	dir := t.TempDir()
	asked = filepath.Join(dir, "go.asked")

	for name, script := range map[string]string{
		"go":       "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$GO_ASKED\"\n",
		"hermetic": "#!/bin/sh\n",
	} {
		// 0o700: a stub is run.
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	return []string{
		"PATH=" + dir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"GO_ASKED=" + asked,
	}, asked
}

// gotools runs limen's verb on root and returns what it reports for the
// gotools rule. The exit status is not read: the fixture is no compliant
// repository, and the other rules say so.
func gotools(t *testing.T, limen string, env []string, verb, root string) report {
	t.Helper()

	out, err := command(t, root, env, limen, verb, "-json", root).Output()

	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		t.Fatalf("%s %s: %v", limen, verb, err)
	}

	var reports []report
	if err := json.Unmarshal(out, &reports); err != nil {
		t.Fatalf("%s %s -json printed no report: %v\n%s", limen, verb, err, out)
	}

	for _, entry := range reports {
		if entry.Rule == "gotools" {
			return entry
		}
	}

	t.Fatalf("%s %s reported nothing for gotools:\n%s", limen, verb, out)

	return report{}
}
