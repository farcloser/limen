// Black-box tests of `limen github signers rollout`, through cli.Run. GitHub
// is two things to the command and both are stood in for: bare repositories
// on disk, which its git clones and pushes to as it would any remote, and an
// API server that answers from those repositories and keeps what it is sent.
// The git is the real one, with a configuration of the test's own: a
// throwaway signing key, so the commits are signed and their signatures read.

package cli_test

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	fleetOrg    = "fleet"
	runnerEmail = "runner@example.test"
	// retiredAt is in the past, as the time of a compromise is when the
	// command is reached for.
	retiredAt = "20260101Z"
)

// signingKey makes a key pair in dir and returns the private key's path,
// the public key as a line of an allowed-signers file wants it (type and
// key), and its fingerprint as ssh-keygen prints it.
func signingKey(t *testing.T, dir, name string) (private, public, fingerprint string) {
	t.Helper()

	private = filepath.Join(dir, name)
	if out, err := sshKeygen(t, nil, "-q", "-t", "ed25519", "-N", "", "-C", "", "-f", private); err != nil {
		t.Fatalf("ssh-keygen: %v\n%s", err, out)
	}

	listed, err := sshKeygen(t, nil, "-l", "-f", private+".pub")
	if err != nil || len(strings.Fields(listed)) < 2 {
		t.Fatalf("ssh-keygen -l: %v\n%s", err, listed)
	}

	text, err := os.ReadFile(private + ".pub")
	if err != nil {
		t.Fatal(err)
	}

	return private, strings.TrimSpace(string(text)), strings.Fields(listed)[1]
}

// gitOut runs git in dir and returns what it printed; a git that fails
// fails the test.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()

	out, err := gitTry(t, dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}

	return out
}

// gitTry runs git in dir and returns what it printed, and whether it failed.
// What it printed is its standard output alone: a hint or a warning git
// writes beside it is no part of a branch's name or a file's content. A git
// that failed answers with what it complained of instead.
func gitTry(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(t.Context(), "git", args...)
	cmd.Dir = dir
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return stdout.String() + stderr.String(), err
	}

	return strings.TrimRight(stdout.String(), "\n"), nil
}

// A fleet is the stand-in for GitHub: an organization's repositories, bare,
// under one directory, and the API that describes them.
type fleet struct {
	t   *testing.T
	dir string

	mu sync.Mutex
	// archived names the repositories the API reports archived.
	archived map[string]bool
	// pulls is the open pull requests, by repository.
	pulls map[string][]map[string]any
	// writes is every write the API was sent, "METHOD path" each.
	writes []string
	// refusePulls is how many pull requests the API still refuses to open.
	refusePulls int
	// beforeOpening, when set, runs once as a pull request is being opened:
	// what happens to a repository between a run's look and its write.
	beforeOpening func()
	// beforeComparing, when set, runs once as two commits are compared:
	// what happens to a repository in the middle of a run's look.
	beforeComparing func()
	// labels is the labels set, by "repository#number".
	labels map[string][]string
}

// newFleet starts the API, points the command at it, and gives git the
// configuration the command's commits are made under: the runner's name and
// email, and the key at signingKeyPath to sign with.
func newFleet(t *testing.T, signingKeyPath string) *fleet {
	t.Helper()

	rig := &fleet{
		t: t, dir: t.TempDir(),
		archived: map[string]bool{}, pulls: map[string][]map[string]any{}, labels: map[string][]string{},
	}

	config := filepath.Join(rig.dir, "gitconfig")
	text := "[user]\n\tname = Runner\n\temail = " + runnerEmail + "\n\tsigningkey = " + filepath.ToSlash(
		signingKeyPath,
	) + "\n" +
		"[gpg]\n\tformat = ssh\n[init]\n\tdefaultBranch = main\n"

	if err := os.WriteFile(config, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("GIT_CONFIG_GLOBAL", config)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")

	server := httptest.NewServer(http.HandlerFunc(rig.serve))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "test")

	return rig
}

// bare is the path of a repository's bare clone, the remote of the command.
func (f *fleet) bare(name string) string { return filepath.Join(f.dir, name+".git") }

// repository creates a repository whose main carries the given
// allowed-signers file, or none when signers is empty.
func (f *fleet) repository(name, signers string) {
	f.t.Helper()

	work := filepath.Join(f.dir, name+".work")

	gitOut(f.t, f.dir, "init", "--quiet", "--bare", f.bare(name))
	gitOut(f.t, f.dir, "init", "--quiet", work)

	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("# "+name+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}

	if signers != "" {
		if err := os.WriteFile(filepath.Join(work, ".lint-signers"), []byte(signers), 0o600); err != nil {
			f.t.Fatal(err)
		}
	}

	gitOut(f.t, work, "add", ".")
	gitOut(f.t, work, "commit", "--quiet", "--no-gpg-sign", "--message", "start")
	gitOut(f.t, work, "push", "--quiet", f.bare(name), "main")
}

// advance gives a repository's main one more commit, which adds a file.
func (f *fleet) advance(name, file string) {
	f.t.Helper()

	work := filepath.Join(f.dir, name+".work")
	if err := os.WriteFile(filepath.Join(work, file), []byte(file+"\n"), 0o600); err != nil {
		f.t.Fatal(err)
	}

	gitOut(f.t, work, "add", ".")
	gitOut(f.t, work, "commit", "--quiet", "--no-gpg-sign", "--message", "add "+file)
	gitOut(f.t, work, "push", "--quiet", f.bare(name), "main")
}

// answer writes a JSON answer.
func answer(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// serve is the API: the endpoints the command reads, answered from the
// bare repositories, and the two it writes to, kept.
func (f *fleet) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	path := strings.TrimPrefix(r.URL.Path, "/")

	if r.Method != http.MethodGet {
		f.writes = append(f.writes, r.Method+" "+path)
	}

	if r.Method == http.MethodGet && path == "orgs/"+fleetOrg+"/repos" {
		f.listRepositories(w)

		return
	}

	// repos/<owner>/<name>/<endpoint>, the owner and the name in any case,
	// as GitHub takes them.
	const withEndpoint = 4

	segments := strings.SplitN(path, "/", withEndpoint)
	for len(segments) < withEndpoint {
		segments = append(segments, "")
	}

	inFleet := segments[0] == "repos" && strings.EqualFold(segments[1], fleetOrg)
	name, endpoint := strings.ToLower(segments[2]), segments[3]

	if _, err := os.Stat(f.bare(name)); !inFleet || err != nil {
		answer(w, http.StatusNotFound, map[string]string{"message": "Not Found"})

		return
	}

	switch {
	case r.Method == http.MethodGet && endpoint == "":
		answer(w, http.StatusOK, map[string]any{
			"default_branch": "main", "ssh_url": f.bare(name), "archived": f.archived[name],
		})
	case r.Method == http.MethodGet && endpoint == "contents/.lint-signers":
		f.contents(w, name, r.URL.Query().Get("ref"))
	case r.Method == http.MethodGet && endpoint == "pulls":
		_, head, _ := strings.Cut(r.URL.Query().Get("head"), ":")
		open := []map[string]any{}

		for _, pull := range f.pulls[name] {
			if pull["head"] == head {
				open = append(open, f.asGitHub(name, pull))
			}
		}

		answer(w, http.StatusOK, open)
	case r.Method == http.MethodGet && strings.HasPrefix(endpoint, "compare/"):
		f.compare(w, name, strings.TrimPrefix(endpoint, "compare/"))
	case r.Method == http.MethodGet && strings.HasPrefix(endpoint, "git/ref/heads/"):
		ref := strings.TrimPrefix(endpoint, "git/ref/")
		if _, err := gitTry(f.t, f.bare(name), "rev-parse", "--verify", "--quiet", "refs/"+ref); err != nil {
			answer(w, http.StatusNotFound, map[string]string{"message": "Not Found"})

			return
		}

		answer(w, http.StatusOK, map[string]string{"ref": "refs/" + ref})
	case r.Method == http.MethodPost && endpoint == "pulls":
		f.openPull(w, r, name)
	case r.Method == http.MethodPost && strings.HasPrefix(endpoint, "issues/") && strings.HasSuffix(endpoint, "/labels"):
		var sent struct {
			Labels []string `json:"labels"`
		}

		_ = json.NewDecoder(r.Body).Decode(&sent)

		number := strings.TrimSuffix(strings.TrimPrefix(endpoint, "issues/"), "/labels")
		f.labels[name+"#"+number] = sent.Labels

		answer(w, http.StatusOK, []any{})
	default:
		f.t.Errorf("the command made a call the fleet does not know: %s %s", r.Method, r.URL.RequestURI())
		answer(w, http.StatusInternalServerError, map[string]string{"message": "unknown call"})
	}
}

// listRepositories answers the organization's listing: every bare
// repository under the fleet's directory.
func (f *fleet) listRepositories(w http.ResponseWriter) {
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		f.t.Error(err)
	}

	listed := []map[string]any{}

	for _, entry := range entries {
		if name, isBare := strings.CutSuffix(entry.Name(), ".git"); isBare {
			listed = append(listed, map[string]any{"name": name, "archived": f.archived[name]})
		}
	}

	answer(w, http.StatusOK, listed)
}

// contents answers the allowed-signers file of a branch as GitHub does:
// base64, broken into lines.
func (f *fleet) contents(w http.ResponseWriter, name, ref string) {
	text, err := gitTry(f.t, f.bare(name), "show", ref+":.lint-signers")
	if err != nil {
		answer(w, http.StatusNotFound, map[string]string{"message": "Not Found"})

		return
	}

	const lineLength = 60

	encoded := base64.StdEncoding.EncodeToString([]byte(text + "\n"))

	var broken strings.Builder

	for len(encoded) > lineLength {
		broken.WriteString(encoded[:lineLength] + "\n")
		encoded = encoded[lineLength:]
	}

	broken.WriteString(encoded + "\n")
	answer(w, http.StatusOK, map[string]string{"encoding": "base64", "content": broken.String()})
}

// openPull opens a pull request, unless the fleet was told to refuse it.
func (f *fleet) openPull(w http.ResponseWriter, r *http.Request, name string) {
	if f.refusePulls > 0 {
		f.refusePulls--

		answer(w, http.StatusInternalServerError, map[string]string{"message": "Server Error"})

		return
	}

	body, _ := io.ReadAll(r.Body)

	pull := map[string]any{}
	if err := json.Unmarshal(body, &pull); err != nil {
		f.t.Errorf("a pull request that is not JSON: %s", body)
	}

	if f.beforeOpening != nil {
		f.beforeOpening()
		f.beforeOpening = nil
	}

	number := len(f.pulls[name]) + 1
	pull["number"] = number
	pull["html_url"] = "https://github.test/" + fleetOrg + "/" + name + "/pull/" + strconv.Itoa(number)
	f.pulls[name] = append(f.pulls[name], pull)

	answer(w, http.StatusCreated, f.asGitHub(name, pull))
}

// asGitHub is a pull request as the API answers it: what was sent, with the
// head as an object that names its repository and the commit it is at now.
func (f *fleet) asGitHub(name string, pull map[string]any) map[string]any {
	branch := fmt.Sprint(pull["head"])
	tip, _ := gitTry(f.t, f.bare(name), "rev-parse", "--verify", "--quiet", "refs/heads/"+branch)

	// A pull request the test planted as a fork's says so in "fork".
	from := fleetOrg + "/" + name
	if fork, planted := pull["fork"]; planted {
		from = fmt.Sprint(fork)
	}

	shown := maps.Clone(pull)
	shown["head"] = map[string]any{"ref": branch, "sha": tip, "repo": map[string]string{"full_name": from}}

	return shown
}

// compare answers what the head has over the base, "base...head": how many
// commits, and which.
func (f *fleet) compare(w http.ResponseWriter, name, spec string) {
	if f.beforeComparing != nil {
		f.beforeComparing()
		f.beforeComparing = nil
	}

	base, head, _ := strings.Cut(spec, "...")

	listed, err := gitTry(f.t, f.bare(name), "rev-list", base+".."+head)
	if err != nil {
		answer(w, http.StatusNotFound, map[string]string{"message": "Not Found"})

		return
	}

	commits := []map[string]string{}
	for _, sha := range slices.Backward(strings.Fields(listed)) {
		commits = append(commits, map[string]string{"sha": sha})
	}

	answer(w, http.StatusOK, map[string]any{"ahead_by": len(commits), "commits": commits})
}

// rollout runs the command against the fleet and returns its exit code and
// what it said, both streams.
func (f *fleet) rollout(args ...string) (int, string) {
	f.t.Helper()

	var stdout, stderr bytes.Buffer

	code := run(append([]string{"github", "signers", "rollout"}, args...), &stdout, &stderr)

	return code, stdout.String() + stderr.String()
}

// branches is the branches of a repository other than main.
func (f *fleet) branches(name string) []string {
	f.t.Helper()

	listed := gitOut(f.t, f.bare(name), "for-each-ref", "--format=%(refname:short)", "refs/heads")

	return slices.DeleteFunc(strings.Fields(listed), func(branch string) bool { return branch == "main" })
}

// written is every write the API was sent so far.
func (f *fleet) written() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.writes)
}

// signersLine is one line of an allowed-signers file, for git.
func signersLine(principal, options, public string) string {
	if options != "" {
		options = "," + options
	}

	return principal + ` namespaces="git"` + options + " " + public + "\n"
}

// onlyPull is the one pull request open in a repository.
func (f *fleet) onlyPull(name string) map[string]any {
	f.t.Helper()

	if len(f.pulls[name]) != 1 {
		f.t.Fatalf("%s has %d pull requests open, want one: %v", name, len(f.pulls[name]), f.pulls[name])
	}

	return f.pulls[name][0]
}

// retireBranch is the one branch a rollout of the key left in a repository.
//
//nolint:unparam // the tests that look happen to name their repository alike; the call says which.
func (f *fleet) retireBranch(name string) string {
	f.t.Helper()

	branches := f.branches(name)
	if len(branches) != 1 {
		f.t.Fatalf("%s has the branches %v beside main, want the rollout's one", name, branches)
	}

	return branches[0]
}

// TestRolloutRetiresWhereTheKeyIsListed: across an organization, the one
// repository that lists the key with its window open gets one commit over
// main, signed by the runner and accepted by the file it edits, on a branch
// of its own, and one pull request with the fixed title and the label; the
// file is what `limen signers retire` writes. The others are passed over,
// each for its reason, the archived one unseen. A second run finds the pull
// request and does nothing. Serial: sets the process environment.
//
//nolint:paralleltest // serial: sets the process environment.
func TestRolloutRetiresWhereTheKeyIsListed(t *testing.T) {
	keys := t.TempDir()
	runnerKey, runnerPublic, _ := signingKey(t, keys, "runner")
	_, oldPublic, oldPrint := signingKey(t, keys, "old")

	rig := newFleet(t, runnerKey)
	runner := signersLine(runnerEmail, "", runnerPublic)
	listed := "# who signs here\n" + signersLine("bot@example.test", "", oldPublic) + runner

	rig.repository("alpha", listed)
	rig.repository("beta", runner)
	rig.repository("gamma", "")
	rig.repository("delta", signersLine("bot@example.test", `valid-before="`+retiredAt+`"`, oldPublic)+runner)
	rig.repository("epsilon", listed)
	rig.archived["epsilon"] = true

	mainBefore := gitOut(t, rig.bare("alpha"), "rev-parse", "main")

	code, said := rig.rollout("-org", fleetOrg, "-valid-before", retiredAt, "-yes", oldPrint)
	if code != 0 {
		t.Fatalf("exit %d, want 0:\n%s", code, said)
	}

	for repo, want := range map[string]string{
		"alpha": "https://github.test/fleet/alpha/pull/1",
		"beta":  "does not list the key",
		"gamma": "no .lint-signers",
		"delta": "already retired at " + retiredAt,
	} {
		if !lineSays(said, fleetOrg+"/"+repo, want) {
			t.Errorf("no line says of %s: %q\n%s", repo, want, said)
		}
	}

	if strings.Contains(said, "epsilon") {
		t.Errorf("an archived repository was looked at:\n%s", said)
	}

	for _, untouched := range []string{"beta", "gamma", "delta", "epsilon"} {
		if branches := rig.branches(untouched); len(branches) != 0 {
			t.Errorf("%s was pushed to: %v", untouched, branches)
		}
	}

	branch := rig.retireBranch("alpha")
	bare := rig.bare("alpha")

	if got := gitOut(t, bare, "rev-parse", "main"); got != mainBefore {
		t.Errorf("main moved: %s, was %s", got, mainBefore)
	}

	if count := gitOut(t, bare, "rev-list", "--count", "main.."+branch); count != "1" {
		t.Errorf("%s is %s commits over main, want 1", branch, count)
	}

	if changed := gitOut(t, bare, "diff", "--name-only", "main", branch); changed != ".lint-signers" {
		t.Errorf("the commit changes %q, want .lint-signers alone", changed)
	}

	// The file is what the local command writes from the same start.
	local := filepath.Join(t.TempDir(), ".lint-signers")
	if err := os.WriteFile(local, []byte(listed), 0o600); err != nil {
		t.Fatal(err)
	}

	if retireCode, out, _ := signers(t, local, "retire", "-valid-before", retiredAt, oldPrint); retireCode != 0 {
		t.Fatalf("limen signers retire: exit %d: %s", retireCode, out)
	}

	want, err := os.ReadFile(local)
	if err != nil {
		t.Fatal(err)
	}

	pushed := gitOut(t, bare, "show", branch+":.lint-signers") + "\n"
	if pushed != string(want) {
		t.Errorf("the file pushed is not what `limen signers retire` writes:\n%s\nwant:\n%s", pushed, want)
	}

	// The commit: the fixed subject, signed off as the runner, and signed by
	// a key the file it carries lists for the committer.
	title := "Retire the signing key " + oldPrint

	message := gitOut(t, bare, "log", "-1", "--format=%s%n%(trailers:key=Signed-off-by,valueonly)", branch)
	if message != title+"\nRunner <"+runnerEmail+">" {
		t.Errorf("the commit's subject and sign-off are:\n%s", message)
	}

	if status := gitOut(
		t,
		bare,
		"-c",
		"gpg.ssh.allowedSignersFile="+local,
		"log",
		"-1",
		"--format=%G?",
		branch,
	); status != "G" {
		t.Errorf("git says %q of the commit's signature under the file it carries, want G", status)
	}

	pull := rig.onlyPull("alpha")
	if pull["title"] != title || pull["head"] != branch || pull["base"] != "main" {
		t.Errorf("the pull request is %v, want %q from %s into main", pull, title, branch)
	}

	if !strings.Contains(fmt.Sprint(pull["body"]), oldPrint) || !strings.Contains(fmt.Sprint(pull["body"]), retiredAt) {
		t.Errorf("the pull request's body names neither the key nor the time: %v", pull["body"])
	}

	if labels := rig.labels["alpha#1"]; !slices.Equal(labels, []string{"internal"}) {
		t.Errorf("the pull request's labels are %v, want internal", labels)
	}

	wrote := []string{"POST repos/fleet/alpha/pulls", "POST repos/fleet/alpha/issues/1/labels"}
	if got := rig.written(); !slices.Equal(got, wrote) {
		t.Errorf("the run wrote %v, want %v", got, wrote)
	}

	// Again: the pull request is found, and nothing more is done.
	head := gitOut(t, bare, "rev-parse", branch)

	code, said = rig.rollout("-org", fleetOrg, "-valid-before", retiredAt, "-yes", oldPrint)
	if code != 0 || !lineSays(said, "fleet/alpha", "pull request open: https://github.test/fleet/alpha/pull/1") ||
		!strings.Contains(said, "nothing to roll out") {
		t.Errorf("a second run: exit %d\n%s", code, said)
	}

	if got := rig.written(); !slices.Equal(got, wrote) {
		t.Errorf("a second run wrote: %v", got[len(wrote):])
	}

	if got := gitOut(t, bare, "rev-parse", rig.retireBranch("alpha")); got != head {
		t.Errorf("a second run moved the branch: %s, was %s", got, head)
	}

	// Named in another case, the repository is the same one to GitHub, and
	// its pull request the same.
	code, said = rig.rollout("-repo", "Fleet/Alpha", "-valid-before", retiredAt, "-yes", oldPrint)
	if code != 0 || !lineSays(said, "Fleet/Alpha", "pull request open: https://github.test/fleet/alpha/pull/1") {
		t.Errorf("named in another case: exit %d\n%s", code, said)
	}

	if got := rig.written(); !slices.Equal(got, wrote) {
		t.Errorf("named in another case, the run wrote: %v", got[len(wrote):])
	}
}

// lineSays reports whether a line of the output names the repository and
// carries the text.
func lineSays(output, repo, text string) bool {
	for line := range strings.SplitSeq(output, "\n") {
		if slices.Contains(strings.Fields(line), repo) && strings.Contains(line, text) {
			return true
		}
	}

	return false
}

// TestRolloutFinishesWhatARunLeft: a run whose pull request GitHub refused
// has pushed its branch, and says it failed. The next run opens the pull
// request of that branch and makes no second commit. Serial: sets the
// process environment.
//
//nolint:paralleltest // serial: sets the process environment.
func TestRolloutFinishesWhatARunLeft(t *testing.T) {
	keys := t.TempDir()
	runnerKey, runnerPublic, _ := signingKey(t, keys, "runner")
	_, oldPublic, oldPrint := signingKey(t, keys, "old")

	rig := newFleet(t, runnerKey)
	rig.repository("alpha", signersLine("bot@example.test", "", oldPublic)+signersLine(runnerEmail, "", runnerPublic))
	rig.refusePulls = 1

	args := []string{"-repo", "fleet/alpha", "-valid-before", retiredAt, "-yes", oldPrint}

	code, said := rig.rollout(args...)
	if code != 1 || !strings.Contains(said, "1 of 1 pull requests were not opened") {
		t.Fatalf("a run GitHub refused the pull request of: exit %d, want 1\n%s", code, said)
	}

	branch := rig.retireBranch("alpha")
	head := gitOut(t, rig.bare("alpha"), "rev-parse", branch)

	if len(rig.pulls["alpha"]) != 0 {
		t.Fatalf("a refused pull request is open: %v", rig.pulls["alpha"])
	}

	code, said = rig.rollout(args...)
	if code != 0 || !lineSays(said, "fleet/alpha", "branch an earlier run pushed") ||
		!lineSays(said, "fleet/alpha", "https://github.test/fleet/alpha/pull/1") {
		t.Fatalf("the next run: exit %d, want 0 and the pull request opened\n%s", code, said)
	}

	if got := gitOut(t, rig.bare("alpha"), "rev-parse", rig.retireBranch("alpha")); got != head {
		t.Errorf("the next run moved the branch: %s, was %s", got, head)
	}

	if pull := rig.onlyPull("alpha"); pull["head"] != branch {
		t.Errorf("the pull request is from %v, want %s", pull["head"], branch)
	}
}

// TestRolloutOfAnotherTimeIsAnotherPullRequest: a retirement is a key and a
// time. With the pull request of one time open, a run for another time
// does not take it for its own: it opens a second, whose file carries the
// second time. Serial: sets the process environment.
//
//nolint:paralleltest // serial: sets the process environment.
func TestRolloutOfAnotherTimeIsAnotherPullRequest(t *testing.T) {
	const earlier = "20251201Z"

	keys := t.TempDir()
	runnerKey, runnerPublic, _ := signingKey(t, keys, "runner")
	_, oldPublic, oldPrint := signingKey(t, keys, "old")

	rig := newFleet(t, runnerKey)
	rig.repository("alpha", signersLine("bot@example.test", "", oldPublic)+signersLine(runnerEmail, "", runnerPublic))

	for _, at := range []string{retiredAt, earlier} {
		if code, said := rig.rollout("-repo", "fleet/alpha", "-valid-before", at, "-yes", oldPrint); code != 0 {
			t.Fatalf("the run for %s: exit %d\n%s", at, code, said)
		}
	}

	if len(rig.pulls["alpha"]) != 2 {
		t.Fatalf("%d pull requests are open, want one for each time", len(rig.pulls["alpha"]))
	}

	second := fmt.Sprint(rig.pulls["alpha"][1]["head"])
	if second == rig.pulls["alpha"][0]["head"] {
		t.Fatalf("the two pull requests share the branch %s", second)
	}

	if file := gitOut(
		t,
		rig.bare("alpha"),
		"show",
		second+":.lint-signers",
	); !strings.Contains(
		file,
		`valid-before="`+earlier+`"`,
	) {
		t.Errorf("the second pull request's file does not carry %s:\n%s", earlier, file)
	}
}

// replant rewrites the retirement's branch of a repository: in a clone of
// its own, with the branch checked out as the remote has it, change does
// what it will, and whatever is then checked out is pushed over the branch.
//
//nolint:unparam // the tests that rewrite happen to name their repository alike; the call says which.
func (f *fleet) replant(name, branch string, change func(work string)) {
	f.t.Helper()

	work := filepath.Join(f.t.TempDir(), "replant")

	gitOut(f.t, f.dir, "clone", "--quiet", "--branch", branch, f.bare(name), work)
	change(work)
	gitOut(f.t, work, "push", "--quiet", "--force", "origin", "HEAD:refs/heads/"+branch)
}

// TestRolloutTakesNoBranchOnItsName: the branch's name is the key and the
// time, which anyone who may push can write. A branch found under it gets a
// pull request, or has its open one counted, only when it is what a run
// pushes: one commit over main, the retirement's edit and nothing else,
// signed by a key the file it carries accepts. Each row starts from a run
// stopped before its pull request, rewrites the branch, and runs again: the
// run must refuse the branch, say why, and write nothing. Serial: sets the
// process environment.
//
//nolint:paralleltest // serial: sets the process environment.
func TestRolloutTakesNoBranchOnItsName(t *testing.T) {
	keys := t.TempDir()
	runnerKey, runnerPublic, _ := signingKey(t, keys, "runner")
	oldKey, oldPublic, oldPrint := signingKey(t, keys, "old")
	otherKey, otherPublic, _ := signingKey(t, keys, "other")

	listed := signersLine("bot@example.test", "", oldPublic) + signersLine(runnerEmail, "", runnerPublic)
	args := []string{"-repo", "fleet/alpha", "-valid-before", retiredAt, "-yes", oldPrint}

	for name, tc := range map[string]struct {
		change func(t *testing.T, work string)
		says   string
	}{
		"another change altogether": {
			func(t *testing.T, work string) {
				t.Helper()

				gitOut(t, work, "reset", "--quiet", "--hard", "origin/main")

				planted := listed + signersLine("other@example.test", "", otherPublic)
				for file, text := range map[string]string{".lint-signers": planted, "planted.txt": "planted\n"} {
					if err := os.WriteFile(filepath.Join(work, file), []byte(text), 0o600); err != nil {
						t.Fatal(err)
					}
				}

				gitOut(t, work, "add", ".")
				gitOut(t, work, "commit", "--quiet", "--no-gpg-sign", "--message", "Retire the signing key "+oldPrint)
			},
			"its commit changes .lint-signers, planted.txt",
		},
		"the edit, unsigned": {
			func(t *testing.T, work string) {
				t.Helper()

				gitOut(t, work, "commit", "--quiet", "--amend", "--no-edit", "--no-gpg-sign")
			},
			"not signed by a key the file lists for the committer",
		},
		"the edit, signed by a key the file does not list": {
			func(t *testing.T, work string) {
				t.Helper()

				gitOut(t, work, "-c", "user.signingkey="+filepath.ToSlash(otherKey),
					"commit", "--quiet", "--amend", "--no-edit", "--gpg-sign")
			},
			"lists no key that signs for " + runnerEmail,
		},
		"the edit, signed by the key it retires": {
			func(t *testing.T, work string) {
				t.Helper()

				gitOut(t, work, "-c", "user.signingkey="+filepath.ToSlash(oldKey),
					"commit", "--quiet", "--amend", "--no-edit", "--gpg-sign")
			},
			"signed by the key being retired",
		},
		"the edit and one more commit": {
			func(t *testing.T, work string) {
				t.Helper()

				gitOut(t, work, "commit", "--quiet", "--allow-empty", "--gpg-sign", "--message", "one more")
			},
			"it is 2 commits over main, not one",
		},
		"the edit at another time": {
			func(t *testing.T, work string) {
				t.Helper()

				gitOut(t, work, "reset", "--quiet", "--hard", "origin/main")

				file := filepath.Join(work, ".lint-signers")
				if code, out, _ := signers(t, file, "retire", "-valid-before", "20250601Z", oldPrint); code != 0 {
					t.Fatalf("limen signers retire: exit %d: %s", code, out)
				}

				gitOut(t, work, "commit", "--quiet", "--all", "--gpg-sign", "--message", "Retire the signing key "+oldPrint)
			},
			"its file is not the one retiring the key gives",
		},
	} {
		t.Run(name, func(t *testing.T) {
			rig := newFleet(t, runnerKey)
			rig.repository("alpha", listed)
			rig.refusePulls = 1

			if code, said := rig.rollout(args...); code != 1 {
				t.Fatalf("the run stopped before its pull request: exit %d\n%s", code, said)
			}

			branch := rig.retireBranch("alpha")
			wrote := rig.written()

			rig.replant("alpha", branch, func(work string) { tc.change(t, work) })

			code, said := rig.rollout(args...)
			if code != 1 || !lineSays(said, "fleet/alpha", "the branch "+branch+" is not what a rollout pushes") ||
				!strings.Contains(said, tc.says) {
				t.Errorf("exit %d, want 1, the branch refused, and %q:\n%s", code, tc.says, said)
			}

			// A branch someone else signed says nothing of this run's key.
			if strings.Contains(said, "stopped") {
				t.Errorf("the run took a found branch's signer for its own:\n%s", said)
			}

			if got := rig.written(); !slices.Equal(got, wrote) {
				t.Errorf("the run wrote for a branch it refused: %v", got[len(wrote):])
			}
		})
	}

	t.Run("with its pull request open", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", listed)

		if code, said := rig.rollout(args...); code != 0 {
			t.Fatalf("the first run: exit %d\n%s", code, said)
		}

		rig.replant("alpha", rig.retireBranch("alpha"), func(work string) {
			gitOut(t, work, "commit", "--quiet", "--allow-empty", "--gpg-sign", "--message", "one more")
		})

		code, said := rig.rollout(args...)
		if code != 1 || !lineSays(said, "fleet/alpha", "is not what a rollout pushes") ||
			!lineSays(said, "fleet/alpha", "has a pull request open: https://github.test/fleet/alpha/pull/1") {
			t.Errorf("exit %d, want 1 and the open pull request named as not the rollout's:\n%s", code, said)
		}
	})

	// A merge that is one commit over main and reads as the edit against its
	// first parent: main as it was a commit ago, with the retirement's file,
	// merged with main's tip. Against main it takes away what main gained
	// since. Only a commit with one parent is made on the default branch's
	// own history.
	t.Run("the edit as a merge that undoes main's last commit", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", listed)
		rig.advance("alpha", "precious.txt")
		rig.refusePulls = 1

		if code, said := rig.rollout(args...); code != 1 {
			t.Fatalf("the run stopped before its pull request: exit %d\n%s", code, said)
		}

		branch := rig.retireBranch("alpha")
		wrote := rig.written()

		rig.replant("alpha", branch, func(work string) {
			edited := gitOut(t, work, "rev-parse", "HEAD:.lint-signers")

			gitOut(t, work, "read-tree", "origin/main~1")
			gitOut(t, work, "update-index", "--cacheinfo", "100644,"+edited+",.lint-signers")

			tree := gitOut(t, work, "write-tree")
			merge := gitOut(t, work, "commit-tree", "-S", "-p", "origin/main~1", "-p", "origin/main",
				"-m", "Retire the signing key "+oldPrint, tree)

			gitOut(t, work, "reset", "--quiet", "--hard", merge)
		})

		bare := rig.bare("alpha")
		if over := gitOut(t, bare, "rev-list", "--count", "main.."+branch); over != "1" {
			t.Fatalf("the merge is %s commits over main, and the row wants it to be one", over)
		}

		if gone := gitOut(
			t,
			bare,
			"diff",
			"--name-status",
			"main",
			branch,
			"--",
			"precious.txt",
		); !strings.HasPrefix(
			gone,
			"D",
		) {
			t.Fatalf("the merge does not take main's last file away (%q), and the row wants it to", gone)
		}

		code, said := rig.rollout(args...)
		if code != 1 || !lineSays(said, "fleet/alpha", "is not what a rollout pushes: its commit is a merge") {
			t.Errorf("exit %d, want 1 and the merge refused:\n%s", code, said)
		}

		if got := rig.written(); !slices.Equal(got, wrote) {
			t.Errorf("the run wrote for a branch it should have refused: %v", got[len(wrote):])
		}
	})

	// Two pushes around the run's look: the branch holds a commit that lists
	// a key of the pusher's and, over it, the honest edit signed with that
	// key, which reads as a rollout's in every way but the count of commits;
	// the rollout's own commit is put back while the count is asked, and the
	// two commits again before the pull request is opened. What is counted
	// must be the commit that was read, whatever the branch is by then.
	t.Run("swapped for the honest commit while its commits are counted", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", listed)
		rig.refusePulls = 1

		if code, said := rig.rollout(args...); code != 1 {
			t.Fatalf("the run stopped before its pull request: exit %d\n%s", code, said)
		}

		branch := rig.retireBranch("alpha")
		bare := rig.bare("alpha")
		honest := gitOut(t, bare, "rev-parse", branch)
		wrote := rig.written()

		rig.replant("alpha", branch, func(work string) {
			gitOut(t, work, "reset", "--quiet", "--hard", "origin/main")

			file := filepath.Join(work, ".lint-signers")
			if err := os.WriteFile(
				file,
				[]byte(listed+signersLine("other@example.test", "", otherPublic)),
				0o600,
			); err != nil {
				t.Fatal(err)
			}

			gitOut(t, work, "commit", "--quiet", "--all", "--no-gpg-sign", "--message", "list a key")

			if code, out, _ := signers(t, file, "retire", "-valid-before", retiredAt, oldPrint); code != 0 {
				t.Fatalf("limen signers retire: exit %d: %s", code, out)
			}

			gitOut(t, work, "-c", "user.signingkey="+filepath.ToSlash(otherKey),
				"commit", "--quiet", "--all", "--gpg-sign", "--message", "Retire the signing key "+oldPrint)
		})

		planted := gitOut(t, bare, "rev-parse", branch)
		rig.beforeComparing = func() { gitOut(t, bare, "update-ref", "refs/heads/"+branch, honest) }
		rig.beforeOpening = func() { gitOut(t, bare, "update-ref", "refs/heads/"+branch, planted) }

		code, said := rig.rollout(args...)
		if code != 1 || !lineSays(said, "fleet/alpha", "is not what a rollout pushes: it is 2 commits over main") {
			t.Errorf("exit %d, want 1 and the two commits counted:\n%s", code, said)
		}

		if got := rig.written(); !slices.Equal(got, wrote) {
			t.Errorf("the run wrote for a branch it should have refused: %v", got[len(wrote):])
		}
	})

	// The API's filter is by owner, and a fork the same owner holds answers
	// to it: its pull request is not the repository's own branch's.
	t.Run("a fork's pull request from a branch of the same name", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", listed)
		rig.refusePulls = 1

		if code, said := rig.rollout(args...); code != 1 {
			t.Fatalf("the run stopped before its pull request: exit %d\n%s", code, said)
		}

		rig.pulls["alpha"] = append(rig.pulls["alpha"], map[string]any{
			"head": rig.retireBranch("alpha"), "fork": "fleet/alpha-fork", "number": 1,
			"html_url": "https://github.test/fleet/alpha/pull/1",
		})

		code, said := rig.rollout(args...)
		if code != 0 || !lineSays(said, "fleet/alpha", "https://github.test/fleet/alpha/pull/2") {
			t.Errorf("exit %d, want 0 and the branch's own pull request opened beside the fork's:\n%s", code, said)
		}
	})

	t.Run("pushed to between the look and the pull request", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", listed)
		rig.refusePulls = 1

		if code, said := rig.rollout(args...); code != 1 {
			t.Fatalf("the run stopped before its pull request: exit %d\n%s", code, said)
		}

		branch := rig.retireBranch("alpha")
		rig.beforeOpening = func() {
			rig.replant("alpha", branch, func(work string) {
				gitOut(t, work, "commit", "--quiet", "--allow-empty", "--gpg-sign", "--message", "one more")
			})
		}

		code, said := rig.rollout(args...)
		if code != 1 || !strings.Contains(said, "the branch moved while its pull request was being opened") ||
			!strings.Contains(said, "https://github.test/fleet/alpha/pull/1") {
			t.Errorf("exit %d, want 1 and the pull request named as one to close:\n%s", code, said)
		}

		if labels := rig.labels["alpha#1"]; len(labels) != 0 {
			t.Errorf("a pull request of a branch that moved was labelled as the rollout's: %v", labels)
		}
	})
}

// TestRolloutPushesNothingItsLintWouldRefuse: a commit the repository's own
// file does not accept is never pushed. The key being retired, once its
// window has ended, cannot sign its own retirement, and the run stops at the
// first repository, since every other would say the same; a runner the file
// does not list is refused there and the run goes on. A file that already
// retires the key at another time is left as it is. Serial: sets the process
// environment.
//
//nolint:paralleltest // serial: sets the process environment.
func TestRolloutPushesNothingItsLintWouldRefuse(t *testing.T) {
	keys := t.TempDir()
	runnerKey, runnerPublic, runnerPrint := signingKey(t, keys, "runner")
	_, oldPublic, oldPrint := signingKey(t, keys, "old")

	runner := signersLine(runnerEmail, "", runnerPublic)
	old := signersLine("bot@example.test", "", oldPublic)

	t.Run("the key being retired signs", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", runner)
		rig.repository("beta", runner)

		code, said := rig.rollout("-org", fleetOrg, "-valid-before", retiredAt, "-yes", runnerPrint)
		if code != 1 || !strings.Contains(said, "fleet/alpha: signed by the key being retired") ||
			!strings.Contains(said, "stopped, 1 repositories not attempted") ||
			!strings.Contains(said, "2 of 2 pull requests were not opened") {
			t.Errorf("exit %d, want 1, the reason, and the run stopped:\n%s", code, said)
		}

		rig.pushedNothing("alpha", "beta")
	})

	t.Run("the runner is not a signer of the repository", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", old)
		rig.repository("beta", old+runner)

		code, said := rig.rollout("-org", fleetOrg, "-valid-before", retiredAt, "-yes", oldPrint)
		if code != 1 || !strings.Contains(said, "fleet/alpha: not signed by a key the file lists for the committer") ||
			!strings.Contains(
				said,
				runnerEmail,
			) || !lineSays(said, "fleet/beta", "https://github.test/fleet/beta/pull/1") {
			t.Errorf("exit %d, want 1, alpha refused and beta opened:\n%s", code, said)
		}

		rig.pushedNothing("alpha")
	})

	t.Run("the key is already retired at another time", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", signersLine("bot@example.test", `valid-before="20250101Z"`, oldPublic)+runner)

		code, said := rig.rollout("-repo", "fleet/alpha", "-valid-before", retiredAt, "-yes", oldPrint)
		if code != 1 || !lineSays(said, "fleet/alpha", "already retired, at 20250101Z") {
			t.Errorf("exit %d, want 1 and the conflict said:\n%s", code, said)
		}

		rig.pushedNothing("alpha")
	})

	t.Run("the repository named is archived", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", old+runner)
		rig.archived["alpha"] = true

		code, said := rig.rollout("-repo", "fleet/alpha", "-valid-before", retiredAt, "-yes", oldPrint)
		if code != 1 || !lineSays(said, "fleet/alpha", "archived") {
			t.Errorf("exit %d, want 1 and the repository said archived:\n%s", code, said)
		}

		rig.pushedNothing("alpha")
	})

	t.Run("no repository lists the key", func(t *testing.T) {
		rig := newFleet(t, runnerKey)
		rig.repository("alpha", runner)
		rig.repository("beta", "")

		code, said := rig.rollout("-org", fleetOrg, "-valid-before", retiredAt, "-yes", oldPrint)
		if code != 1 || !strings.Contains(said, "no repository lists "+oldPrint) {
			t.Errorf("exit %d, want 1 and the fingerprint questioned:\n%s", code, said)
		}

		rig.pushedNothing("alpha", "beta")
	})
}

// pushedNothing fails the test when a repository has a branch beside main,
// or the API was sent a write.
func (f *fleet) pushedNothing(names ...string) {
	f.t.Helper()

	for _, name := range names {
		if branches := f.branches(name); len(branches) != 0 {
			f.t.Errorf("%s was pushed to: %v", name, branches)
		}
	}

	wrote := f.written()
	for _, name := range names {
		for _, write := range wrote {
			if strings.Contains(write, "/"+name+"/") {
				f.t.Errorf("the API was sent a write for %s: %s", name, write)
			}
		}
	}
}

// TestRolloutUsage: the target is named, one of the two; the time is given,
// and is one; the fingerprint is the one argument. Nothing is read before
// that holds. Serial: sets the process environment.
func TestRolloutUsage(t *testing.T) {
	const unknown = "SHA256:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++

		http.Error(w, `{"message":"not asked for"}`, http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	t.Setenv("GITHUB_API_URL", server.URL)
	t.Setenv("GH_TOKEN", "test")

	for name, args := range map[string][]string{
		"no target":                 {"github", "signers", "rollout", "-valid-before", retiredAt, unknown},
		"two targets":               {"github", "signers", "rollout", "-org", "a", "-repo", "a/b", "-valid-before", retiredAt, unknown},
		"no time":                   {"github", "signers", "rollout", "-org", "a", unknown},
		"no fingerprint":            {"github", "signers", "rollout", "-org", "a", "-valid-before", retiredAt},
		"two fingerprints":          {"github", "signers", "rollout", "-org", "a", "-valid-before", retiredAt, unknown, unknown},
		"a local time":              {"github", "signers", "rollout", "-org", "a", "-valid-before", "20260101", unknown},
		"no command":                {"github", "signers"},
		"a command that is not one": {"github", "signers", "retire"},
	} {
		var stdout, stderr bytes.Buffer
		if code := run(args, &stdout, &stderr); code != 2 || stdout.Len() != 0 {
			t.Errorf("%s: exit %d, want 2 and nothing on stdout:\n%s%s", name, code, stdout.String(), stderr.String())
		}
	}

	if calls != 0 {
		t.Errorf("the API was called %d times by runs that had nothing to ask", calls)
	}
}
