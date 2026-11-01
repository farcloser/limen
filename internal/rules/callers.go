package rules

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/farcloser/limen"
)

// The canonical workflows whose steps run from limen's repository. The file a
// repository carries is a thin caller, content-pinned but for the one line
// Renovate moves: the `uses:` reference to the reusable workflow at a commit
// of farcloser/limen. A fix-up on a Renovate branch thereby never writes a
// workflow file, which the run's own token may not; limen's own repository
// calls the reusable workflows locally, since it is where they live.
const (
	limenRepository      = "farcloser/limen"
	reusableSecurity     = "limen-security.yaml"
	reusableCaches       = "limen-caches.yaml"
	reusableChecksum     = "limen-update-aqua-checksum.yaml"
	reusableVerify       = "limen-verify.yaml"
	devBuildCallerNotice = "a development build seeds no workflow caller: the reference is a release's commit"
)

// caller is one thin caller: the path every repository carries, the reusable
// workflow it calls and limen's own copy, which calls it locally.
type caller struct {
	path      string
	reusable  string
	canonical string
}

// callers is every thin caller, in the order fix reports them.
func callers() []caller {
	return []caller{
		{path: pathWorkflowSecurity, reusable: reusableSecurity, canonical: limen.CanonicalWorkflowSecurity},
		{path: pathWorkflowCaches, reusable: reusableCaches, canonical: limen.CanonicalWorkflowCaches},
		{path: pathWorkflowChecksum, reusable: reusableChecksum, canonical: limen.CanonicalWorkflowUpdateAquaChecksum},
	}
}

// reusablePath is a reusable workflow's path in the repository.
func reusablePath(reusable string) string { return workflowsDir + "/" + reusable }

// usesPattern matches the `uses:` line calling the named reusable workflow,
// locally or at a commit of farcloser/limen with Renovate's version comment,
// capturing the indentation and the reference.
func usesPattern(reusable string) *regexp.Regexp {
	escaped := regexp.QuoteMeta(reusablePath(reusable))

	return regexp.MustCompile(
		`(?m)^(\s+uses: )(\./` + escaped + `|` + regexp.QuoteMeta(limenRepository) + `/` + escaped +
			`@[0-9a-f]{` + "40" + `}(?: # \S+)?)[ \t]*$`,
	)
}

// localUses is the reference limen's own files carry.
func localUses(reusable string) string { return "./" + reusablePath(reusable) }

// remoteUses is the reference a repository carries: the reusable workflow at
// the commit of the limen release, with the version Renovate reads.
func remoteUses(reusable, revision, version string) string {
	return limenRepository + "/" + reusablePath(reusable) + "@" + revision + " # " + version
}

// isLocalUses reports whether a reference is the local form.
func isLocalUses(ref string) bool { return strings.HasPrefix(ref, "./") }

// usesIn returns the reference the file carries for the reusable workflow,
// or "" when it has no such line.
func usesIn(content, reusable string) string {
	match := usesPattern(reusable).FindStringSubmatch(content)
	if match == nil {
		return ""
	}

	return match[2]
}

// withUses returns content with its reference to the reusable workflow
// replaced by ref.
func withUses(content, reusable, ref string) string {
	return usesPattern(reusable).ReplaceAllString(content, "${1}"+ref)
}

// callsLocally reports whether a repository may call the reusable workflows
// by their local path: limen itself, and a repository whose name is unknown
// (no origin to read), which is not judged.
func callsLocally(repository string) bool {
	return repository == "" || repository == limenRepository
}

// callerDrift reports whether the file at content is the canonical caller
// but for its reference, and if not why. A local reference is drift outside
// limen: the reusable workflow is not there.
func callerDrift(content string, item caller, repository string) (string, bool) {
	ref := usesIn(content, item.reusable)
	if ref == "" {
		return "does not call " + item.reusable + " (content-pinned but for the uses: reference Renovate moves)", true
	}

	if isLocalUses(ref) && !callsLocally(repository) {
		return "calls " + ref + ", which only " + limenRepository + " carries: the reference is " +
			limenRepository + "/" + reusablePath(item.reusable) + "@<commit> (limen fix sets it)", true
	}

	if codeLines(withUses(content, item.reusable, localUses(item.reusable))) != codeLines(item.canonical) {
		return "does not match the canonical caller (content-pinned but for the uses: reference Renovate moves)", true
	}

	return "", false
}

// codeLines is content without its comment-only lines and trailing comments:
// what a caller does, not what it says. A caller is held to that, so a
// comment edited in limen never asks a repository for a workflow write.
func codeLines(content string) string {
	var kept []string

	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSuffix(line, carriageReturn)
		if code, _, _ := strings.Cut(line, " #"); strings.TrimSpace(code) != "" &&
			!strings.HasPrefix(strings.TrimSpace(code), "#") {
			kept = append(kept, strings.TrimRight(code, " \t"))
		}
	}

	return strings.Join(kept, "\n")
}

// callerRef is the reference a fix writes: the file's own when it is
// acceptable, the local one in limen, the release's commit elsewhere; "" when
// a development build would have to make one up.
func callerRef(existing, reusable, repository, revision, version string) string {
	if existing != "" && (!isLocalUses(existing) || callsLocally(repository)) {
		return existing
	}

	if callsLocally(repository) {
		return localUses(reusable)
	}

	if revision == "" {
		return ""
	}

	return remoteUses(reusable, revision, version)
}

// remediateCaller seeds or resets one thin caller, keeping the reference the
// file carries when it is acceptable: that line is Renovate's.
func remediateCaller(root, rule string, item caller, opts FixOptions) Outcome {
	data, err := readRepoFile(root, item.path)
	content := string(data)

	if err == nil {
		if _, drifted := callerDrift(content, item, opts.Policy.Repository); !drifted {
			return Outcome{Rule: rule, Action: ActionNone, Path: item.path, Message: item.path + matchesCanonicalMsg}
		}
	}

	// On a Renovate branch the fix-up is the run's own token's commit, which
	// may not write a workflow file: the caller is left as it is and check
	// says what it needs, for a hand push.
	if opts.RenovateBranch {
		return Outcome{
			Rule: rule, Action: ActionAdvisory, Path: item.path,
			Message: item.path + " drifted from the canonical caller; a Renovate branch's fix-up may not write a " +
				"workflow file, so it is left for a hand push (limen check names it)",
		}
	}

	ref := callerRef(
		usesIn(content, item.reusable),
		item.reusable,
		opts.Policy.Repository,
		opts.SelfRevision,
		opts.SelfVersion,
	)
	if ref == "" {
		return Outcome{
			Rule:    rule,
			Action:  ActionAdvisory,
			Path:    item.path,
			Message: item.path + ": " + devBuildCallerNotice,
		}
	}

	if e := writeFile(root, item.path, withUses(item.canonical, item.reusable, ref)); e != nil {
		return failed(rule, item.path, e)
	}

	if err != nil {
		return Outcome{
			Rule:    rule,
			Action:  ActionCreated,
			Path:    item.path,
			Message: "wrote the canonical caller " + item.path,
		}
	}

	return Outcome{
		Rule:    rule,
		Action:  ActionOverwrote,
		Path:    item.path,
		Message: "reset " + item.path + " to the canonical caller",
	}
}

// CanonicalCallersFor returns the thin callers as `limen fix` writes them in
// the named repository at the given release commit and version: limen's own
// files, calling locally, for limen itself. Exported for the command's
// tests, which build compliant repositories from the canonical files.
func CanonicalCallersFor(repository, revision, version string) map[string]string {
	out := map[string]string{}

	for _, item := range callers() {
		ref := localUses(item.reusable)
		if !callsLocally(repository) {
			ref = remoteUses(item.reusable, revision, version)
		}

		out[item.path] = withUses(item.canonical, item.reusable, ref)
	}

	out[pathWorkflowCI] = CanonicalCIFor(repository, revision, version)

	return out
}

// CanonicalCIFor returns the ci.yaml seed for the named repository: calling
// limen-verify.yaml locally in limen, at the release commit elsewhere.
func CanonicalCIFor(repository, revision, version string) string {
	if callsLocally(repository) {
		return limen.CanonicalWorkflowCI
	}

	return withUses(limen.CanonicalWorkflowCI, reusableVerify, remoteUses(reusableVerify, revision, version))
}

// ciCallsLocalVerify reports whether a repository's own ci.yaml still calls
// limen-verify.yaml by its local path, the one line of that file that is
// limen's to move: outside limen the file is not there.
func ciCallsLocalVerify(root, repository string) bool {
	if callsLocally(repository) {
		return false
	}

	data, err := readRepoFile(root, pathWorkflowCI)

	return err == nil && isLocalUses(usesIn(string(data), reusableVerify))
}

// strayVerify reports whether a repository carries limen-verify.yaml while
// not being limen: a reusable workflow nothing calls, from before the lanes
// were called at a commit of farcloser/limen.
func strayVerify(root, repository string) bool {
	return !callsLocally(repository) && exists(filepath.Join(root, filepath.FromSlash(reusablePath(reusableVerify))))
}

// removeStrayVerify deletes the stray; the caller has established there is one.
func removeStrayVerify(root, rule string) Outcome {
	path := reusablePath(reusableVerify)
	if err := os.Remove(filepath.Join(root, filepath.FromSlash(path))); err != nil {
		return failed(rule, path, err)
	}

	return Outcome{
		Rule: rule, Action: ActionDeleted, Path: path,
		Message: "removed " + path + ": the lanes are called at a commit of " + limenRepository + " now",
	}
}

// checkCallers fails a missing or drifted thin caller, and a copy of
// limen-verify.yaml outside limen; nil when all is well.
func checkCallers(root, rule, repository string) *Finding {
	for _, item := range callers() {
		data, err := readRepoFile(root, item.path)
		if err != nil {
			missing := fail(rule, "", item.path+" is missing")

			return &missing
		}

		if why, drifted := callerDrift(string(data), item, repository); drifted {
			drift := fail(rule, item.path, item.path+" "+why)

			return &drift
		}
	}

	if strayVerify(root, repository) {
		stray := fail(
			rule,
			reusablePath(reusableVerify),
			reusablePath(reusableVerify)+" is limen's reusable workflow, called at a "+
				"commit of "+limenRepository+" by ci.yaml now: a copy here runs nothing (limen fix removes it)",
		)

		return &stray
	}

	return nil
}
