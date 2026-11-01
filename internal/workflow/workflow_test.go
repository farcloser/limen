package workflow_test

import (
	"slices"
	"testing"

	"github.com/farcloser/limen/internal/workflow"
)

const release = `name: release

on:
  push:
    tags: ["v*"]

jobs:
  sign:
    runs-on: macos-15
    environment: release
    steps:
      - name: Signing identity
        env:
          P12_BASE64: ${{ secrets.APPLE_DEVELOPER_ID_P12 }}
          P12_PASSWORD: ${{ secrets.APPLE_DEVELOPER_ID_P12_PASSWORD }}
        run: |
          # a shell comment, and secrets.yaml is a file name
          cat secrets.yaml
      - env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: gh release upload
  publish:
    runs-on: ubuntu-24.04
    steps:
      - run: just do release --ci
`

func TestParseRelease(t *testing.T) {
	t.Parallel()

	read := workflow.Parse([]byte(release))

	if len(read.Triggers) != 1 || read.Triggers[0].Name != "push" || !read.Triggers[0].TagsOnly {
		t.Fatalf("triggers: %+v, want one push confined to tags", read.Triggers)
	}

	if read.OpenTrigger() != "" {
		t.Errorf("a push confined to tags is not open: %q", read.OpenTrigger())
	}

	if len(read.Jobs) != 2 {
		t.Fatalf("jobs: %+v, want sign and publish", read.Jobs)
	}

	sign := read.Jobs[0]
	if sign.Name != "sign" || sign.Environment != "release" {
		t.Errorf("sign: %+v", sign)
	}

	want := []string{"APPLE_DEVELOPER_ID_P12", "APPLE_DEVELOPER_ID_P12_PASSWORD"}
	if !slices.Equal(sign.Secrets, want) || sign.AllSecrets {
		t.Errorf(
			"sign secrets: %v all=%v, want %v (GITHUB_TOKEN and the file name aside)",
			sign.Secrets,
			sign.AllSecrets,
			want,
		)
	}

	if publish := read.Jobs[1]; publish.HoldsSecrets() || publish.Environment != "" {
		t.Errorf("publish: %+v, want no secret and no environment", publish)
	}
}

func TestTriggerForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want []workflow.Trigger
		open string
	}{
		{"scalar", "on: push\n", []workflow.Trigger{{Name: "push"}}, "push"},
		{"quoted key", "\"on\": pull_request\n", []workflow.Trigger{{Name: "pull_request"}}, "pull_request"},
		{
			"flow sequence", "on: [push, pull_request]\n",
			[]workflow.Trigger{{Name: "push"}, {Name: "pull_request"}},
			"push",
		},
		{
			"flow mapping", "on: {push: {tags: [v*]}, workflow_dispatch: {}}\n",
			[]workflow.Trigger{{Name: "push", TagsOnly: true}, {Name: "workflow_dispatch"}},
			"workflow_dispatch",
		},
		{
			"block list", "on:\n  - push\n  - schedule\n",
			[]workflow.Trigger{{Name: "push"}, {Name: "schedule"}},
			"push",
		},
		{
			"list at the key's indentation", "on:\n- release\n",
			[]workflow.Trigger{{Name: "release"}},
			"",
		},
		{
			"push to branches and tags", "on:\n  push:\n    branches: [main]\n    tags: [\"v*\"]\n",
			[]workflow.Trigger{{Name: "push"}},
			"push",
		},
		{
			"tags-ignore alone confines to tags",
			"on:\n  push:\n    tags-ignore:\n      - nightly\n  schedule:\n    - cron: '0 0 * * *'\n",
			[]workflow.Trigger{{Name: "push", TagsOnly: true}, {Name: "schedule"}},
			"",
		},
		{
			"four-space indentation", "on:\n    pull_request_target:\n        types: [opened]\n",
			[]workflow.Trigger{{Name: "pull_request_target"}},
			"pull_request_target",
		},
		{"comment after the key", "on: push # the release button\n", []workflow.Trigger{{Name: "push"}}, "push"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			read := workflow.Parse([]byte(tc.text))
			if !slices.Equal(read.Triggers, tc.want) {
				t.Errorf("triggers: %+v, want %+v", read.Triggers, tc.want)
			}

			if read.OpenTrigger() != tc.open {
				t.Errorf("open trigger: %q, want %q", read.OpenTrigger(), tc.open)
			}
		})
	}
}

func TestJobForms(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		text string
		want workflow.Job
	}{
		{
			"environment as a block with a url",
			"jobs:\n  deploy:\n    environment:\n      name: production\n      url: https://example.test\n    steps: []\n",
			workflow.Job{Name: "deploy", Environment: "production"},
		},
		{
			"environment as a flow mapping",
			"jobs:\n  deploy:\n    environment: { name: 'staging', url: x }\n",
			workflow.Job{Name: "deploy", Environment: "staging"},
		},
		{
			"secrets inherited by a reusable call",
			"jobs:\n  call:\n    uses: org/repo/.github/workflows/x.yaml@abc\n    secrets: inherit\n",
			workflow.Job{Name: "call", AllSecrets: true},
		},
		{
			"secrets passed to a reusable call",
			"jobs:\n  call:\n    uses: ./.github/workflows/x.yaml\n    secrets:\n      token: ${{ secrets.NPM_TOKEN }}\n",
			workflow.Job{Name: "call", Secrets: []string{"NPM_TOKEN"}},
		},
		{
			"the whole context",
			"jobs:\n  dump:\n    steps:\n      - run: echo '${{ toJSON(secrets) }}'\n",
			workflow.Job{Name: "dump", AllSecrets: true},
		},
		{
			"an indexed read",
			"jobs:\n  pick:\n    steps:\n      - run: echo ${{ secrets[format('KEY_{0}', matrix.os)] }}\n",
			workflow.Job{Name: "pick", AllSecrets: true},
		},
		{
			"an expression over two lines",
			"jobs:\n  two:\n    env:\n      A: ${{\n        secrets.A }}\n",
			workflow.Job{Name: "two", Secrets: []string{"A"}},
		},
		{
			"a secret named twice is one",
			"jobs:\n  twice:\n    env:\n      A: ${{ secrets.A }}\n      B: ${{ secrets.A }}\n",
			workflow.Job{Name: "twice", Secrets: []string{"A"}},
		},
		{
			"the token alone is no secret",
			"jobs:\n  token:\n    env:\n      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}\n      T: ${{ github.token }}\n",
			workflow.Job{Name: "token"},
		},
		{
			"another context's field named secrets",
			"jobs:\n  input:\n    env:\n      X: ${{ inputs.secrets }}\n",
			workflow.Job{Name: "input"},
		},
		{
			"a quoted hash is not a comment",
			"jobs:\n  hash:\n    env:\n      X: \"#${{ secrets.A }}\"\n",
			workflow.Job{Name: "hash", Secrets: []string{"A"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			read := workflow.Parse([]byte(tc.text))
			if len(read.Jobs) != 1 {
				t.Fatalf("jobs: %+v, want one", read.Jobs)
			}

			got := read.Jobs[0]
			if got.Name != tc.want.Name || got.Environment != tc.want.Environment ||
				got.AllSecrets != tc.want.AllSecrets || !slices.Equal(got.Secrets, tc.want.Secrets) {
				t.Errorf("job: %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSecretsStayInTheirJob(t *testing.T) {
	t.Parallel()

	text := "jobs:\n  a:\n    env:\n      X: ${{ secrets.A }}\n  b:\n    runs-on: x\n  c:\n    env:\n      Y: ${{ secrets.C }}\n"

	read := workflow.Parse([]byte(text))
	if len(read.Jobs) != 3 {
		t.Fatalf("jobs: %+v", read.Jobs)
	}

	if read.Jobs[1].HoldsSecrets() {
		t.Errorf("b holds its neighbours' secrets: %+v", read.Jobs[1])
	}

	if !slices.Equal(read.Jobs[2].Secrets, []string{"C"}) {
		t.Errorf("c: %+v", read.Jobs[2])
	}
}

func TestFlowStyleJobsReadAsNone(t *testing.T) {
	t.Parallel()

	if read := workflow.Parse([]byte("on: push\njobs: {a: {runs-on: x}}\n")); len(read.Jobs) != 0 {
		t.Errorf("flow-style jobs: %+v, want none (the package comment's limit)", read.Jobs)
	}
}

// TestSigningSecrets: each prefix of the signing policy is recognised on its
// own, and a name outside it is not.
func TestSigningSecrets(t *testing.T) {
	t.Parallel()

	job := workflow.Job{Secrets: []string{
		"NPM_TOKEN", "APPLE_NOTARY_KEY", "APPLE_PROVISIONING_PROFILE", "APPLE_DEVELOPER_ID_P12", "APPLE_TEAM",
	}}

	want := []string{"APPLE_NOTARY_KEY", "APPLE_PROVISIONING_PROFILE", "APPLE_DEVELOPER_ID_P12"}
	if got := job.SigningSecrets(); !slices.Equal(got, want) {
		t.Errorf("signing secrets: %v, want %v", got, want)
	}
}

// TestCalledOnly: a workflow with workflow_call as its one trigger runs in
// its callers' repositories; one that is also pushed runs here too.
func TestCalledOnly(t *testing.T) {
	t.Parallel()

	if !workflow.Parse([]byte("on:\n  workflow_call:\n    secrets:\n      A:\n        required: true\n")).CalledOnly() {
		t.Error("a workflow_call block is called only")
	}

	if workflow.Parse([]byte("on: [workflow_call, push]\n")).CalledOnly() {
		t.Error("a workflow also pushed is not called only")
	}

	if workflow.Parse([]byte("jobs:\n  a:\n    runs-on: x\n")).CalledOnly() {
		t.Error("a workflow with no trigger is not called only")
	}
}

// TestJobUses: the reusable workflow a job calls is read as written.
func TestJobUses(t *testing.T) {
	t.Parallel()

	read := workflow.Parse([]byte(
		"on: push\njobs:\n  release:\n    uses: farcloser/limen/.github/workflows/limen-release-apple.yaml@0123 # v1\n",
	))
	if len(read.Jobs) != 1 || read.Jobs[0].Uses != "farcloser/limen/.github/workflows/limen-release-apple.yaml@0123" {
		t.Errorf("jobs: %+v, want the uses reference without its comment", read.Jobs)
	}
}
