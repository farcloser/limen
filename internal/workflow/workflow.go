// Package workflow reads what a GitHub Actions workflow file says about who
// reaches its secrets: the events that start it and, per job, the environment
// the job deploys to and the secrets its expressions read.
//
// The reader follows the file's indentation, not a YAML grammar: a key is
// read where a workflow author writes one, as a block mapping's entry or a
// one-line flow value. That is every workflow limen seeds and every one the
// fleet carries; a workflow written in flow style throughout (`jobs: {…}`)
// reads as one with no jobs, an `environment:` given as an expression reads
// as a name of that text, and a `#` after an unmatched apostrophe on the line
// is not stripped as a comment.
package workflow

import (
	"regexp"
	"slices"
	"strings"
)

// Workflow is one workflow file, read for its triggers and its jobs.
type Workflow struct {
	Triggers []Trigger
	Jobs     []Job
}

// Trigger is one event under `on:`.
type Trigger struct {
	Name string
	// TagsOnly is a push trigger with a tags or tags-ignore filter and no
	// branches filter, which GitHub runs for no branch push.
	TagsOnly bool
}

// Job is one entry under `jobs:`.
type Job struct {
	Name string
	// Environment is the job's `environment:`, by name; empty when none.
	Environment string
	// Secrets are the names the job reads from the secrets context,
	// GITHUB_TOKEN aside, in order of first appearance.
	Secrets []string
	// AllSecrets is a reference to the whole context: `secrets: inherit`,
	// `toJSON(secrets)`, `secrets[expression]`.
	AllSecrets bool
}

// ReleaseEnvironment is the environment the signing policy names: it admits
// release tags alone and waits for an owner (book/releasing.md).
const ReleaseEnvironment = "release"

// SigningSecrets are the secrets among the job's that the signing policy
// covers, each the release environment's wherever a job reads it.
func (j Job) SigningSecrets() []string {
	var found []string

	for _, name := range j.Secrets {
		for _, prefix := range []string{"APPLE_DEVELOPER_ID_", "APPLE_NOTARY_", "APPLE_PROVISIONING_"} {
			if strings.HasPrefix(name, prefix) {
				found = append(found, name)

				break
			}
		}
	}

	return found
}

// The events that start a workflow on a ref anyone with write access pushes:
// their own branch, through a pull request, a dispatch or the push itself. A
// push confined to tags is not one, a tag ruleset decides who creates them;
// the events that run on the default branch (a schedule, a release, an
// issue) run what a reviewed merge put there.
const (
	triggerPush               = "push"
	triggerPullRequest        = "pull_request"
	triggerPullRequestTarget  = "pull_request_target"
	triggerWorkflowDispatch   = "workflow_dispatch"
	tokenSecret               = "GITHUB_TOKEN" // #nosec G101 -- the token's name, which no job reads as a secret.
	keyOn, keyJobs            = "on", "jobs"
	keyEnvironment, keyName   = "environment", "name"
	keySecrets, valueInherit  = "secrets", "inherit"
	keyBranches, keyBranchesX = "branches", "branches-ignore"
	keyTags, keyTagsX         = "tags", "tags-ignore"
)

// Open reports whether the trigger starts the workflow from a ref anyone
// with write access pushes.
func (t Trigger) Open() bool {
	switch t.Name {
	case triggerPullRequest, triggerPullRequestTarget, triggerWorkflowDispatch:
		return true
	case triggerPush:
		return !t.TagsOnly
	default:
		return false
	}
}

// OpenTrigger is the name of the workflow's first open trigger, or "".
func (w Workflow) OpenTrigger() string {
	for _, trigger := range w.Triggers {
		if trigger.Open() {
			return trigger.Name
		}
	}

	return ""
}

// HoldsSecrets reports whether the job reads any secret.
func (j Job) HoldsSecrets() bool { return j.AllSecrets || len(j.Secrets) > 0 }

// Parse reads a workflow file.
func Parse(data []byte) Workflow {
	lines := scan(string(data))

	var workflow Workflow

	for index, entry := range lines {
		if entry.indent != 0 || entry.item {
			continue
		}

		switch entry.key {
		case keyOn:
			workflow.Triggers = triggers(lines, index)
		case keyJobs:
			workflow.Jobs = jobs(lines, index)
		}
	}

	return workflow
}

// line is one non-blank line of the file with its comment removed: its
// indentation, whether it is a `- ` list entry, and the key and value when
// it is a mapping entry.
type line struct {
	text   string
	key    string
	value  string
	indent int
	item   bool
}

// keyValueRE matches a mapping entry: a plain or quoted scalar key, the
// colon, and whatever follows it on the line.
var keyValueRE = regexp.MustCompile(`^(?:"([^"]+)"|'([^']+)'|([A-Za-z0-9_.\-]+))\s*:(?:\s+(.*))?$`)

// scan splits the file into its non-blank, comment-stripped lines.
func scan(text string) []line {
	var lines []line

	for raw := range strings.SplitSeq(text, "\n") {
		stripped := strings.TrimRight(stripComment(raw), " \t\r")
		if strings.TrimSpace(stripped) == "" {
			continue
		}

		entry := line{indent: len(stripped) - len(strings.TrimLeft(stripped, " ")), text: strings.TrimSpace(stripped)}

		if entry.text == "-" || strings.HasPrefix(entry.text, "- ") {
			entry.item = true
			entry.text = strings.TrimSpace(strings.TrimPrefix(entry.text, "-"))
		}

		if match := keyValueRE.FindStringSubmatch(entry.text); match != nil {
			entry.key = match[1] + match[2] + match[3]
			entry.value = strings.TrimSpace(match[4])
		}

		lines = append(lines, entry)
	}

	return lines
}

// stripComment removes a `#` comment: one at the line's start or after
// whitespace, outside quotes.
func stripComment(raw string) string {
	var quote byte

	for index := range len(raw) {
		char := raw[index]

		switch {
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '"' || char == '\'':
			quote = char
		case char == '#' && (index == 0 || raw[index-1] == ' ' || raw[index-1] == '\t'):
			return raw[:index]
		}
	}

	return raw
}

// block is the range of lines nested under lines[parent]: every following
// line indented deeper, and a list entry at the parent's own indentation,
// which YAML allows under a mapping key.
func block(lines []line, parent int) (start, end int) {
	indent := lines[parent].indent
	end = parent + 1

	for end < len(lines) && (lines[end].indent > indent || (lines[end].indent == indent && lines[end].item)) {
		end++
	}

	return parent + 1, end
}

// children are the indices of the entries directly under lines[parent]: the
// lines of its block at the block's own, shallowest, indentation.
func children(lines []line, parent int) []int {
	start, end := block(lines, parent)
	if start >= end {
		return nil
	}

	indent := lines[start].indent

	var entries []int

	for index := start; index < end; index++ {
		if lines[index].indent == indent {
			entries = append(entries, index)
		}
	}

	return entries
}

// triggers reads the `on:` entry at lines[index], in its three forms: a
// scalar, a flow sequence or mapping, a block of entries or list items.
func triggers(lines []line, index int) []Trigger {
	value := lines[index].value

	switch {
	case strings.HasPrefix(value, "["):
		var found []Trigger
		for _, name := range flowItems(value) {
			found = append(found, Trigger{Name: name})
		}

		return found
	case strings.HasPrefix(value, "{"):
		var found []Trigger
		for _, entry := range flowEntries(value) {
			found = append(found, Trigger{Name: entry.key, TagsOnly: tagsOnly(flowKeys(entry.value))})
		}

		return found
	case value != "":
		return []Trigger{{Name: unquote(value)}}
	}

	var found []Trigger

	for _, child := range children(lines, index) {
		entry := lines[child]
		if entry.item {
			found = append(found, Trigger{Name: unquote(entry.text)})

			continue
		}

		found = append(found, Trigger{Name: entry.key, TagsOnly: tagsOnly(keysOf(lines, child))})
	}

	return found
}

// tagsOnly is a push filter set with tags and no branches.
func tagsOnly(keys []string) bool {
	tags, branches := false, false

	for _, key := range keys {
		switch key {
		case keyTags, keyTagsX:
			tags = true
		case keyBranches, keyBranchesX:
			branches = true
		}
	}

	return tags && !branches
}

// keysOf are the keys of the entries directly under lines[index], or of its
// one-line flow mapping.
func keysOf(lines []line, index int) []string {
	if value := lines[index].value; strings.HasPrefix(value, "{") {
		return flowKeys(value)
	}

	var keys []string

	for _, child := range children(lines, index) {
		if lines[child].key != "" {
			keys = append(keys, lines[child].key)
		}
	}

	return keys
}

// jobs reads the entries under `jobs:` at lines[index].
func jobs(lines []line, index int) []Job {
	var found []Job

	for _, child := range children(lines, index) {
		if lines[child].key == "" || lines[child].item {
			continue
		}

		found = append(found, job(lines, child))
	}

	return found
}

// job reads one job: its environment from its own entries, its secrets
// from every expression in its block.
func job(lines []line, index int) Job {
	read := Job{Name: lines[index].key}

	for _, child := range children(lines, index) {
		entry := lines[child]

		switch entry.key {
		case keyEnvironment:
			read.Environment = environmentName(lines, child)
		case keySecrets:
			if entry.value == valueInherit {
				read.AllSecrets = true
			}
		}
	}

	start, end := block(lines, index)

	var text strings.Builder

	for _, entry := range lines[start:end] {
		text.WriteString(entry.text)
		text.WriteByte('\n')
	}

	read.Secrets, read.AllSecrets = secretsIn(text.String(), read.AllSecrets)

	return read
}

// environmentName reads `environment:` as a scalar, a flow mapping with a
// name, or a block with a name entry.
func environmentName(lines []line, index int) string {
	value := lines[index].value

	if strings.HasPrefix(value, "{") {
		for _, entry := range flowEntries(value) {
			if entry.key == keyName {
				return unquote(entry.value)
			}
		}

		return ""
	}

	if value != "" {
		return unquote(value)
	}

	for _, child := range children(lines, index) {
		if lines[child].key == keyName {
			return unquote(lines[child].value)
		}
	}

	return ""
}

// expressionRE is one `${{ … }}` expression; secretsRE is a read of the
// secrets context inside it, with the name when one is named.
var (
	expressionRE = regexp.MustCompile(`(?s)\$\{\{(.*?)\}\}`)
	secretsRE    = regexp.MustCompile(`(?:^|[^.\w])secrets\b(?:\.([A-Za-z_][A-Za-z0-9_]*))?`)
)

// secretsIn collects the secrets the expressions in text read.
func secretsIn(text string, all bool) ([]string, bool) {
	var names []string

	for _, expression := range expressionRE.FindAllStringSubmatch(text, -1) {
		for _, read := range secretsRE.FindAllStringSubmatch(expression[1], -1) {
			switch name := read[1]; {
			case name == "":
				all = true
			case name == tokenSecret:
			case !slices.Contains(names, name):
				names = append(names, name)
			}
		}
	}

	return names, all
}

// unquote strips one pair of matching quotes.
func unquote(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') && value[len(value)-1] == value[0] {
		return value[1 : len(value)-1]
	}

	return value
}

// flowEntry is one `key: value` of a flow mapping, the value's own text.
type flowEntry struct {
	key   string
	value string
}

// flowItems splits a flow sequence `[a, b]` into its items.
func flowItems(value string) []string {
	var items []string

	for _, item := range splitFlow(strings.TrimSuffix(strings.TrimPrefix(value, "["), "]")) {
		items = append(items, unquote(item))
	}

	return items
}

// flowEntries splits a flow mapping `{a: x, b: {…}}` into its entries.
func flowEntries(value string) []flowEntry {
	var entries []flowEntry

	for _, item := range splitFlow(strings.TrimSuffix(strings.TrimPrefix(value, "{"), "}")) {
		key, rest, _ := strings.Cut(item, ":")
		entries = append(entries, flowEntry{key: unquote(key), value: strings.TrimSpace(rest)})
	}

	return entries
}

// flowKeys are a flow mapping's keys; nil for anything else.
func flowKeys(value string) []string {
	if !strings.HasPrefix(value, "{") {
		return nil
	}

	var keys []string

	for _, entry := range flowEntries(value) {
		keys = append(keys, entry.key)
	}

	return keys
}

// splitFlow splits on the commas at nesting depth zero.
func splitFlow(text string) []string {
	var (
		items []string
		depth int
		start int
	)

	for index := range len(text) {
		switch text[index] {
		case '[', '{':
			depth++
		case ']', '}':
			depth--
		case ',':
			if depth == 0 {
				items = append(items, text[start:index])
				start = index + 1
			}
		}
	}

	if item := strings.TrimSpace(text[start:]); item != "" {
		items = append(items, item)
	}

	return items
}
