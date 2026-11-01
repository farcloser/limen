package rules

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/farcloser/limen/internal/workflow"
)

// The secrets rule: a workflow job that reads a secret holds it behind an
// environment, so that only the refs the environment admits run with it
// (book/releasing.md, the signing-credential policy, and book/github.md's
// environments check, which verifies the gate on GitHub's side). A job on an
// open trigger (a pull request, a dispatch, a push to a branch) with a secret
// and no environment runs that secret for anyone with write access, from a
// branch of their own whose copy of the workflow prints it.
const (
	ruleSecrets        = "secrets"
	secretsPassMessage = "no workflow job reads a secret where a writable ref reaches it" // #nosec G101 -- a verdict.
)

// checkSecrets is the first job that reads a secret outside its environment,
// or the pass.
func checkSecrets(root string) Finding {
	if f := secretsFinding(root); f != nil {
		return *f
	}

	return Finding{Rule: ruleSecrets, Status: StatusOK, Path: workflowsDir, Message: secretsPassMessage}
}

// remediateSecrets: the environment a job needs is the project's to name,
// so the finding is an advisory carrying the check's message.
func remediateSecrets(root string) Outcome {
	if f := secretsFinding(root); f != nil {
		return Outcome{Rule: ruleSecrets, Action: ActionAdvisory, Path: f.Path, Message: f.Message}
	}

	return Outcome{Rule: ruleSecrets, Action: ActionNone, Path: workflowsDir, Message: secretsPassMessage}
}

// secretsFinding walks the workflows in name order and returns the first
// offending job, nil when none.
func secretsFinding(root string) *Finding {
	for _, path := range workflowFiles(root) {
		data, err := readRepoFile(root, path)
		if err != nil {
			continue
		}

		read := workflow.Parse(data)
		open := read.OpenTrigger()

		for _, job := range read.Jobs {
			if msg := jobSecretsProblem(job, open); msg != "" {
				f := fail(ruleSecrets, path, msg)

				return &f
			}
		}
	}

	return nil
}

// jobSecretsProblem is the message for a job that reads a secret where it
// may not: a signing credential outside the release environment, or any
// secret with no environment on an open trigger; "" when the job is fine.
func jobSecretsProblem(job workflow.Job, open string) string {
	if !job.HoldsSecrets() {
		return ""
	}

	if signing := job.SigningSecrets(); len(signing) > 0 && job.Environment != workflow.ReleaseEnvironment {
		return "job " + job.Name + " reads " + strings.Join(
			signing,
			", ",
		) + " outside the " + workflow.ReleaseEnvironment +
			" environment: a signing credential is that environment's secret, which admits release tags" +
			" and waits for the human (`environment: " + workflow.ReleaseEnvironment + "` on the job;" +
			" `limen github fix` builds the environment) — book/releasing.md"
	}

	if job.Environment == "" && open != "" {
		return "job " + job.Name + " reads " + secretNames(job) + " and runs on " + open +
			", which anyone with write access starts from a branch of their own: an `environment:` on the" +
			" job, gated to the refs that may use the secret (`limen github check` verifies the gate)"
	}

	return ""
}

// secretNames phrases what the job reads.
func secretNames(job workflow.Job) string {
	if job.AllSecrets {
		return "every secret"
	}

	return strings.Join(job.Secrets, ", ")
}

// workflowFiles are the repository's workflow files, slash-separated and
// relative to root, in name order.
func workflowFiles(root string) []string {
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(workflowsDir)))
	if err != nil {
		return nil
	}

	var files []string

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || (!strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml")) {
			continue
		}

		files = append(files, workflowsDir+"/"+name)
	}

	sort.Strings(files)

	return files
}
