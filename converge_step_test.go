package limen_test

import (
	"archive/zip"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// The checksum workflow's finish job commits what a project's converge
// recipe changed, with a token that can write, from an archive made by a job
// that ran the project's code. Its commit step decides which names reach the
// branch, and nothing else in the repository runs it: these tests lift the
// step's script out of the workflow file and run it against repositories and
// archives made for the purpose, a hostile archive's included.

const checksumWorkflow = ".github/workflows/limen-update-aqua-checksum.yaml"

// liftStep returns the script of one step of the checksum workflow: the
// `run: |` block of the step of job that holds marker, a line of the step as
// the file writes it. The file is read by its indentation, a job two spaces
// in and a step's keys eight.
func liftStep(t *testing.T, job, marker string) string {
	t.Helper()

	return liftStepFrom(t, checksumWorkflow, job, marker)
}

// liftStepFrom is liftStep for any workflow file written the same way.
func liftStepFrom(t *testing.T, workflow, job, marker string) string {
	t.Helper()

	data, err := os.ReadFile(workflow)
	if err != nil {
		t.Fatal(err)
	}

	const (
		stepStart = "      - "
		runKey    = "        run: |"
		body      = "          "
	)

	lines := strings.Split(string(data), "\n")
	inJob, inStep, inRun := false, false, false

	var script []string

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") && strings.TrimSpace(line) != "":
			inJob, inStep, inRun = line == "  "+job+":", false, false
		case !inJob:
		case inRun && (strings.HasPrefix(line, body) || strings.TrimSpace(line) == ""):
			script = append(script, strings.TrimPrefix(line, body))
		case inRun:
			return strings.Join(script, "\n") + "\n"
		case strings.HasPrefix(line, stepStart):
			inStep = line == stepStart+marker
		case strings.TrimSpace(line) == marker:
			inStep = true
		case inStep && line == runKey:
			inRun = true
		}
	}

	if len(script) == 0 {
		t.Fatalf("%s: no run block found for the step of job %s that holds %q", workflow, job, marker)
	}

	return strings.Join(script, "\n") + "\n"
}

// stepTools is a directory of stand-ins the steps find first on their PATH.
// curl is one everywhere: it keeps the payload it was handed and answers as
// GitHub does a commit that landed. The steps are written for the Linux
// runner; on macOS two tools they call differ from GNU's (base64 has no -w,
// mktemp does not read TMPDIR), and a stand-in gives each the GNU behaviour
// the script asks for.
func stepTools(t *testing.T) string {
	t.Helper()

	tools := map[string]string{
		"curl": `#!/bin/sh
for arg in "$@"; do case "$arg" in @*) cp "${arg#@}" "$CURL_PAYLOAD" ;; esac; done
printf '{"data":{"createCommitOnBranch":{"commit":{"oid":"0123456789abcdef"}}}}'
`,
	}

	if runtime.GOOS == "darwin" {
		tools["base64"] = "#!/bin/sh\n[ \"$1\" = \"-w0\" ] && shift\n/usr/bin/base64 -i \"$1\" | tr -d '\\n'\n"
		tools["mktemp"] = "#!/bin/sh\nexec /usr/bin/mktemp -d \"${TMPDIR%/}/step.XXXXXX\"\n"
	}

	dir := t.TempDir()

	for name, script := range tools {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// requireRunnerTools skips where the checksum workflow's jobs do not run,
// and fails where they would and a tool they call is missing.
func requireRunnerTools(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("the checksum workflow's jobs run on Linux")
	}

	for _, tool := range []string{"bash", "git", "jq", "python3", "cmp"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("the step calls %s, which is not on the PATH", tool)
		}
	}
}

// convergeTree is what every fixture repository tracks, beside its
// .converge.
func convergeTree() map[string]string {
	return map[string]string{
		"gen/a.txt":                "a\n",
		"gen/b.txt":                "b\n",
		"gen/sub/c.txt":            "c\n",
		"gen/\u00e9.txt":           "e\n",
		"generated/z.txt":          "z\n",
		"single.lock":              "lock\n",
		"\u00e9.lock":              "e\n",
		"other.txt":                "other\n",
		".github/workflows/x.yaml": "on: push\n",
		".limen/pinned.yaml":       "pins\n",
		"real/r.txt":               "r\n",
		"d/k.txt":                  "k\n",
		"xy/k.txt":                 "k\n",
		"src/main.go":              "package main\n",
		"uni/\u00e9.txt":           "e\n",
	}
}

// convergeRepo makes a committed repository of convergeTree, a link to a
// directory and one to a tracked file, and the given .converge.
func convergeRepo(t *testing.T, declared string) string {
	t.Helper()

	dir := t.TempDir()

	files := convergeTree()
	files[".converge"] = declared

	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	for link, target := range map[string]string{"linked": "real", "filelink": "single.lock"} {
		if err := os.Symlink(target, filepath.Join(dir, link)); err != nil {
			t.Fatal(err)
		}
	}

	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "core.quotePath", "true"},
		{"add", "-A"},
		{"commit", "-q", "-m", "fixture"},
	} {
		run(t, dir, nil, "git", args...)
	}

	return dir
}

// convergeArchive writes the artifact as the finish job downloads it: one
// archive under <temp>/converge, holding the status the collecting step
// wrote, NUL-ended entries, and the files it copied under files/.
func convergeArchive(t *testing.T, temp string, status []string, files map[string]string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Join(temp, "converge"), 0o700); err != nil {
		t.Fatal(err)
	}

	out, err := os.Create(filepath.Join(temp, "converge", "converge.zip"))
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = out.Close() }()

	archive := zip.NewWriter(out)

	members := map[string]string{"status": ""}
	for _, entry := range status {
		members["status"] += entry + "\x00"
	}

	for name, content := range files {
		members["files/"+name] = content
	}

	for name, content := range members {
		member, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}

		// A member whose name ends in a slash is a directory to the writer,
		// which takes no bytes for it: the hostile archive's bare `files/`.
		if strings.HasSuffix(name, "/") {
			continue
		}

		if _, err := member.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}

	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
}

// The createCommitOnBranch request, as far as the tests read it.
type commitRequest struct {
	Variables commitVariables `json:"variables"`
}

type commitVariables struct {
	Input commitInput `json:"input"`
}

type commitInput struct {
	FileChanges commitFileChanges `json:"fileChanges"`
}

type commitFileChanges struct {
	Additions []commitAddition `json:"additions"`
	Deletions []commitDeletion `json:"deletions"`
}

type commitAddition struct {
	Path     string `json:"path"`
	Contents string `json:"contents"`
}

type commitDeletion struct {
	Path string `json:"path"`
}

// committed is what the commit step asked GitHub to do: the paths it adds,
// each with its bytes, and the paths it deletes. sent is false when the step
// sent nothing.
type committed struct {
	additions map[string]string
	deletions []string
	sent      bool
}

// runCommitStep runs the finish job's commit step in repo, with the
// artifact under temp, and returns its exit status, its output and what it
// sent.
func runCommitStep(t *testing.T, script, tools, repo, temp string) (failed bool, output string, sent committed) {
	t.Helper()

	payload := filepath.Join(temp, "payload.json")
	stepFile := filepath.Join(temp, "step.sh")

	if err := os.WriteFile(stepFile, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := command(t, repo, []string{
		"PATH=" + tools + string(os.PathListSeparator) + os.Getenv("PATH"),
		"RUNNER_TEMP=" + temp, "GITHUB_REPOSITORY=example/fixture", "BRANCH=renovate/fixture",
		"TOKEN=unused", "GITHUB_OUTPUT=" + filepath.Join(temp, "output"), "CURL_PAYLOAD=" + payload,
	}, "bash", "--noprofile", "--norc", "-eo", "pipefail", stepFile)

	out, runErr := cmd.CombinedOutput()

	var exit *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exit) {
		t.Fatalf("the step did not run: %v", runErr)
	}

	data, readErr := os.ReadFile(payload)
	if readErr != nil {
		return runErr != nil, string(out), committed{}
	}

	var request commitRequest
	if err := json.Unmarshal(data, &request); err != nil {
		t.Fatalf("the payload is not JSON: %v\n%s", err, data)
	}

	sent = committed{additions: map[string]string{}, sent: true}

	for _, addition := range request.Variables.Input.FileChanges.Additions {
		content, err := base64.StdEncoding.DecodeString(addition.Contents)
		if err != nil {
			t.Fatalf("an addition's contents are not base64: %v", err)
		}

		sent.additions[addition.Path] = string(content)
	}

	for _, deletion := range request.Variables.Input.FileChanges.Deletions {
		sent.deletions = append(sent.deletions, deletion.Path)
	}

	slices.Sort(sent.deletions)

	return runErr != nil, string(out), sent
}

// A convergeCase is one run of the commit step: what the project declares,
// what the recipe's job handed over, and what must come of it.
type convergeCase struct {
	name     string
	declared string
	status   []string
	files    map[string]string
	// refused is a part of the message the step fails with; empty when the
	// run is carried.
	refused string
	// A carried run sends additions and deletions, or nothing when both are
	// nil.
	additions map[string]string
	deletions []string
	// silent is text the step's output must not hold.
	silent string
}

const declaredFileAndDir = "single.lock\ngen/\n"

// carriedConvergeCases are runs the step commits, or finds nothing to
// commit in.
func carriedConvergeCases() []convergeCase {
	return []convergeCase{
		{
			name:     "a change, new files and a deletion under a directory, a declared file beside them",
			declared: declaredFileAndDir,
			status: []string{
				" M gen/a.txt",
				"?? gen/new.txt",
				"?? gen/deep/er/n.txt",
				" D gen/b.txt",
				" M single.lock",
			},
			files: map[string]string{
				"gen/a.txt": "A\n", "gen/new.txt": "n\n", "gen/deep/er/n.txt": "d\n", "single.lock": "LOCK\n",
			},
			additions: map[string]string{
				"gen/a.txt": "A\n", "gen/new.txt": "n\n", "gen/deep/er/n.txt": "d\n", "single.lock": "LOCK\n",
			},
			deletions: []string{"gen/b.txt"},
		},
		{
			name: "a deletion alone", declared: declaredFileAndDir,
			status: []string{" D gen/sub/c.txt"}, additions: map[string]string{}, deletions: []string{"gen/sub/c.txt"},
		},
		{
			name: "the same bytes under a directory", declared: declaredFileAndDir,
			status: []string{" M gen/a.txt"}, files: map[string]string{"gen/a.txt": "a\n"},
		},
		{
			name: "the same bytes in a declared file", declared: declaredFileAndDir,
			status: []string{" M single.lock"}, files: map[string]string{"single.lock": "lock\n"},
		},
		{
			name: "a declared file deleted is withheld", declared: declaredFileAndDir,
			status: []string{" D single.lock"},
		},
		{
			name: "a nested directory declared, a change inside it", declared: "gen/sub/\n",
			status: []string{"?? gen/sub/n.txt"}, files: map[string]string{"gen/sub/n.txt": "x"},
			additions: map[string]string{"gen/sub/n.txt": "x"},
		},
		{
			name:     "a deletion of a file whose name git quotes",
			declared: declaredFileAndDir,
			status: []string{
				" D gen/\u00e9.txt",
			},
			additions: map[string]string{},
			deletions: []string{"gen/\u00e9.txt"},
		},
		{
			name: "a declared directory whose only tracked file has a name git quotes", declared: "uni/\n",
			status: []string{"?? uni/n.txt"}, files: map[string]string{"uni/n.txt": "n"},
			additions: map[string]string{"uni/n.txt": "n"},
		},
		{
			name: "a declared file whose name git quotes", declared: "\u00e9.lock\n",
			status: []string{" M \u00e9.lock"}, files: map[string]string{"\u00e9.lock": "E\n"},
			additions: map[string]string{"\u00e9.lock": "E\n"},
		},
		{
			name: "a backslash and a space in names under a directory", declared: declaredFileAndDir,
			status:    []string{`?? gen/a\b.txt`, "?? gen/new dir/n file.txt"},
			files:     map[string]string{`gen/a\b.txt`: "x", "gen/new dir/n file.txt": "y"},
			additions: map[string]string{`gen/a\b.txt`: "x", "gen/new dir/n file.txt": "y"},
		},
	}
}

// hostileConvergeCases are runs whose archive names what the project did
// not declare: each is refused whole.
func hostileConvergeCases() []convergeCase {
	const undeclared = "does not declare"

	named := func(name, entry string, files map[string]string) convergeCase {
		return convergeCase{
			name:     name,
			declared: declaredFileAndDir,
			status:   []string{entry},
			files:    files,
			refused:  undeclared,
		}
	}

	cases := []convergeCase{
		named("traversal out of the directory", "?? gen/../.github/workflows/evil.yaml",
			map[string]string{"gen/../.github/workflows/evil.yaml": "x"}),
		named("an empty segment", "?? gen//x", map[string]string{"gen//x": "x"}),
		named("a dot segment", "?? gen/./x", map[string]string{"gen/./x": "x"}),
		named("a trailing dot-dot", "?? gen/a.txt/..", nil),
		named("git's own directory inside", "?? gen/.git/config", map[string]string{"gen/.git/config": "x"}),
		named("git's own directory in upper case", "?? gen/x/.GIT/y", map[string]string{"gen/x/.GIT/y": "x"}),
		named("a tab in a name", "?? gen/a\tb", map[string]string{"gen/a\tb": "x"}),
		named(
			"a sibling whose name starts the same",
			"?? generated/new.txt",
			map[string]string{"generated/new.txt": "x"},
		),
		named("a file outside any declaration", " M other.txt", map[string]string{"other.txt": "x"}),
		named("an absolute path", "?? /etc/passwd", nil),
		named("the directory itself", "?? gen/", nil),
		named("a workflow file", "?? .github/workflows/new.yaml", map[string]string{".github/workflows/new.yaml": "x"}),
		named("a pin of the toolset", " M .limen/pinned.yaml", map[string]string{".limen/pinned.yaml": "x"}),
		// Each line of such a name would be a pattern of its own to grep, and
		// one of them is declared.
		named("a name holding a newline, one line of it a declared file", "?? junk\nsingle.lock", nil),
		named("a name that would match a declared file as a pattern", "?? .*", map[string]string{".*": "x"}),
		named("an entry with no name", "?? ", nil),
		named("an entry of two characters", "??", nil),
		named("an entry whose third character is no space", "??Xgen/a.txt", map[string]string{"gen/a.txt": "A"}),
		named("an entry whose letters are no status letters", "zz gen/a.txt", map[string]string{"gen/a.txt": "A"}),
		named("a staged rename, whose second field has no status", "R  gen/new.txt\x00gen/a.txt",
			map[string]string{"gen/new.txt": "a\n"}),
		{
			name: "a change outside a nested declared directory", declared: "gen/sub/\n",
			status: []string{"?? gen/sub/n.txt", " M gen/a.txt"},
			files:  map[string]string{"gen/sub/n.txt": "x", "gen/a.txt": "A\n"}, refused: undeclared,
		},
		{
			name: "a carriage return in an undeclared name stays out of the log", declared: declaredFileAndDir,
			status: []string{"?? oth\rer.txt"}, refused: undeclared, silent: "\r",
		},
		{
			name: "under a directory, in the status as neither a file nor a deletion", declared: declaredFileAndDir,
			status: []string{"?? gen/link"}, refused: "neither a file the artifact carries nor a deletion",
		},
		{
			name: "a deletion of what the branch does not track", declared: declaredFileAndDir,
			status: []string{" D gen/ghost.txt"}, refused: "not a file this branch tracks",
		},
	}

	// A control character among an entry's first three: the entries kept for
	// the directories are read back by lines, so a newline there would make
	// another name of what follows it, one nothing checked. With a short
	// declared directory that name can be any path.
	for _, control := range []string{"\n", "\r", "\t", "\x01", "\x7f"} {
		for position := range 3 {
			letters := []string{"A", "M", " "}
			letters[position] = control

			cases = append(cases, convergeCase{
				name: "the control character " + strconv.Quote(control) + " at position " +
					strconv.Itoa(position) + " of an entry, short directories declared",
				declared: "single.lock\ngen/\nd/\nxy/\n",
				status: []string{
					strings.Join(letters, "") + "d/.limen/pinned.yaml",
					strings.Join(letters, "") + "xy/src/main.go",
				},
				files: map[string]string{
					".limen/pinned.yaml": "x",
					"src/main.go":        "evil",
					"/src/main.go":       "evil",
					"":                   "y",
				},
				refused: undeclared,
			})
		}
	}

	return cases
}

// badDeclarationCases are .converge files the step refuses before it reads
// the archive.
func badDeclarationCases() []convergeCase {
	const (
		forbidden = "is not a path a derived file may have"
		noDir     = "is not a directory of tracked files under that exact name"
		noFile    = "is not a tracked regular file under that exact name"
	)

	declarations := map[string]string{
		".github/": forbidden, ".github/workflows/": forbidden, ".git/": forbidden, ".limen/": forbidden,
		".hermetic/": forbidden, ".limen/pinned.yaml": forbidden,
		"./gen/": noDir, "gen//": noDir, "gen/../gen/": noDir, "src/../gen/": noDir, "g*n/": noDir, "*/": noDir,
		"nodir/": noDir, "single.lock/": noDir, "linked/": noDir, "/": noDir, "./": noDir, ".GITHUB/": noDir,
		"gen": noFile, "./single.lock": noFile, "s*.lock": noFile, "linked": noFile, "absent.lock": noFile,
		"filelink": noFile,
	}

	cases := make([]convergeCase, 0, len(declarations))
	for declared, refused := range declarations {
		cases = append(cases, convergeCase{name: "declares " + declared, declared: declared + "\n", refused: refused})
	}

	return cases
}

// TestConvergeCommitStep runs the finish job's commit step, as the workflow
// file has it, against each case: what it sends is exactly what the case
// allows, and a refusal sends nothing.
func TestConvergeCommitStep(t *testing.T) {
	t.Parallel()
	requireRunnerTools(t)

	script := liftStep(t, "finish", "id: commit")
	tools := stepTools(t)

	for _, tc := range slices.Concat(carriedConvergeCases(), hostileConvergeCases(), badDeclarationCases()) {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			repo, temp := convergeRepo(t, tc.declared), t.TempDir()
			convergeArchive(t, temp, tc.status, tc.files)

			failed, output, sent := runCommitStep(t, script, tools, repo, temp)

			if tc.silent != "" && strings.Contains(output, tc.silent) {
				t.Errorf("the output holds %q:\n%q", tc.silent, output)
			}

			if tc.refused != "" {
				if !failed || !strings.Contains(output, tc.refused) || sent.sent {
					t.Fatalf("want a refusal saying %q and nothing sent; failed=%v sent=%+v\n%s",
						tc.refused, failed, sent, output)
				}

				return
			}

			if failed {
				t.Fatalf("the step failed:\n%s", output)
			}

			if tc.additions == nil && tc.deletions == nil {
				if sent.sent {
					t.Fatalf("nothing to commit, and the step sent %+v", sent)
				}

				return
			}

			if !sent.sent {
				t.Fatalf("the step sent nothing:\n%s", output)
			}

			if !mapsEqual(sent.additions, tc.additions) || !slices.Equal(sent.deletions, tc.deletions) {
				t.Errorf("sent additions %q and deletions %q, want %q and %q",
					sent.additions, sent.deletions, tc.additions, tc.deletions)
			}
		})
	}
}

func mapsEqual(got, want map[string]string) bool {
	if len(got) != len(want) {
		return false
	}

	for key, value := range want {
		if found, ok := got[key]; !ok || found != value {
			return false
		}
	}

	return true
}

// TestConvergeCollectsWhatTheCommitStepReads runs the converge job's
// collecting step over a tree a recipe changed, packs what it left as the
// upload does, and hands it to the commit step in a second checkout: the two
// steps agree on the artifact's shape, names with spaces included.
func TestConvergeCollectsWhatTheCommitStepReads(t *testing.T) {
	t.Parallel()
	requireRunnerTools(t)

	tools := stepTools(t)
	recipeRepo, collected := convergeRepo(t, declaredFileAndDir), t.TempDir()

	for name, content := range map[string]string{
		"gen/a.txt": "A\n", "gen/new dir/n file.txt": "n\n", "single.lock": "LOCK\n",
	} {
		path := filepath.Join(recipeRepo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if err := os.Remove(filepath.Join(recipeRepo, "gen", "b.txt")); err != nil {
		t.Fatal(err)
	}

	collectFile := filepath.Join(collected, "collect.sh")
	if err := os.WriteFile(
		collectFile,
		[]byte(liftStep(t, "converge", "name: Collect what it changed")),
		0o600,
	); err != nil {
		t.Fatal(err)
	}

	run(t, recipeRepo, []string{
		"PATH=" + tools + string(os.PathListSeparator) + os.Getenv("PATH"), "RUNNER_TEMP=" + collected,
	}, "bash", "--noprofile", "--norc", "-eo", "pipefail", collectFile)

	status, err := os.ReadFile(filepath.Join(collected, "converge", "status"))
	if err != nil {
		t.Fatal(err)
	}

	files := map[string]string{}
	root := filepath.Join(collected, "converge", "files")

	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}

		content, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}

		relative, relErr := filepath.Rel(root, path)
		files[filepath.ToSlash(relative)] = string(content)

		return relErr
	})
	if err != nil {
		t.Fatal(err)
	}

	temp := t.TempDir()
	convergeArchive(t, temp, strings.Split(strings.TrimSuffix(string(status), "\x00"), "\x00"), files)

	failed, output, sent := runCommitStep(
		t,
		liftStep(t, "finish", "id: commit"),
		tools,
		convergeRepo(t, declaredFileAndDir),
		temp,
	)
	if failed {
		t.Fatalf("the commit step failed on what the collecting step made:\n%s", output)
	}

	want := map[string]string{"gen/a.txt": "A\n", "gen/new dir/n file.txt": "n\n", "single.lock": "LOCK\n"}
	if !mapsEqual(sent.additions, want) || !slices.Equal(sent.deletions, []string{"gen/b.txt"}) {
		t.Errorf("sent additions %q and deletions %q, want %q and [gen/b.txt]", sent.additions, sent.deletions, want)
	}
}
