package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCheckProseFindsBrokenConventions asserts that each prose rule reports the
// line that breaks it and leaves the writing the conventions allow alone. The
// cases that matter are the ones where the same text is a defect in one place
// and correct in another: a long line is only a defect outside a fence and
// outside a table row, a `-` only outside the checkbox form, and a banned
// character is a defect wherever it appears because a command carrying one is
// broken rather than merely styled differently.
func TestCheckProseFindsBrokenConventions(t *testing.T) {
	overLimit := "The reconciler holds the lock for " + strings.Repeat("a very long time ", 4)
	underLimit := "The reconciler holds the lock until it finishes."

	tests := []struct {
		name     string
		markdown string
		want     []string
	}{
		{
			name:     "an em dash in prose is reported",
			markdown: "The controller \u2014 which holds the lock \u2014 acknowledges it.\n",
			want: []string{
				"page.md:1: dash or arrow character: em dash (U+2014) at column 16",
				"page.md:1: dash or arrow character: em dash (U+2014) at column 39",
			},
		},
		{
			name:     "an en dash and an arrow are reported by name",
			markdown: "Ports 1323\u20131324 map \u2192 the API and \u21d2 the agent.\n",
			want: []string{
				"page.md:1: dash or arrow character: en dash (U+2013) at column 11",
				"page.md:1: dash or arrow character: rightwards arrow (U+2192) at column 21",
				"page.md:1: dash or arrow character: rightwards double arrow (U+21D2) at column 35",
			},
		},
		{
			name:     "a banned character inside a fence is still reported",
			markdown: "Run the reconciler.\n\n```bash\ntptctl get workloads \u2192 output\n```\n",
			want:     []string{"page.md:4: dash or arrow character: rightwards arrow (U+2192) at column 22"},
		},
		{
			name:     "prose past the column limit is reported",
			markdown: overLimit + "\n",
			want:     []string{"page.md:1: line length: 102 columns, wrap at 80"},
		},
		{
			name:     "prose within the column limit passes",
			markdown: underLimit + "\n",
			want:     []string{},
		},
		{
			name:     "a long line inside a fence passes",
			markdown: "Run the reconciler.\n\n```bash\n" + overLimit + "\n```\n",
			want:     []string{},
		},
		{
			name:     "a list item holding only a long link passes",
			markdown: "  * [kubernetes-workload-controller](../../cmd/kubernetes-workload-controller/README.md)\n",
			want:     []string{},
		},
		{
			name:     "a long link with words after it is reported",
			markdown: "  * [kubernetes-workload-controller](../../cmd/kubernetes-workload-controller/README.md) manages workloads\n",
			want:     []string{"page.md:1: line length: 106 columns, wrap at 80"},
		},
		{
			name:     "a long table row passes",
			markdown: "| Field | Meaning |\n|---|---|\n| Reconciled | " + overLimit + " |\n",
			want:     []string{},
		},
		{
			name:     "one space between sentences is reported",
			markdown: "The lock expires. The message is requeued.\n",
			want:     []string{`page.md:1: sentence spacing: one space in "s. T", use two`},
		},
		{
			name:     "two spaces between sentences pass",
			markdown: "The lock expires.  The message is requeued.\n",
			want:     []string{},
		},
		{
			name:     "an ordered list marker is not a sentence break",
			markdown: "1. Spin up a control plane.\n1. Check the tables.\n",
			want:     []string{},
		},
		{
			name:     "a heading deeper than three levels is reported",
			markdown: "#### Lock Buckets\n",
			want:     []string{"page.md:1: heading depth: 4 levels deep, go no deeper than 3"},
		},
		{
			name:     "a third level heading passes",
			markdown: "### Lock Buckets\n",
			want:     []string{},
		},
		{
			name:     "a hyphen list marker is reported",
			markdown: "* the API server\n- the controllers\n+ the agent\n",
			want: []string{
				"page.md:2: bullet marker: list item opens with -, use *",
				"page.md:3: bullet marker: list item opens with +, use *",
			},
		},
		{
			name:     "the checkbox form passes",
			markdown: "- [] Cut the release.\n- [x] Update the version.\n",
			want:     []string{},
		},
		{
			name:     "a prompt inside a fence is reported",
			markdown: "Create the workload.\n\n```bash\n$ tptctl create kubernetes-workload\n```\n",
			want:     []string{"page.md:4: prompt character: command carries a $ prompt"},
		},
		{
			name:     "a bare command inside a fence passes",
			markdown: "Create the workload.\n\n```bash\ntptctl create kubernetes-workload\n```\n",
			want:     []string{},
		},
		{
			name:     "a fence directly under a heading is reported",
			markdown: "## Installation\n\n```bash\nmage build:tptctl\n```\n",
			want:     []string{`page.md:3: fence lead-in: fence opens directly under "## Installation"`},
		},
		{
			name:     "a fence under a sentence passes",
			markdown: "## Installation\n\nBuild the command line tool.\n\n```bash\nmage build:tptctl\n```\n",
			want:     []string{},
		},
		{
			name:     "a second fence is judged by its own lead-in",
			markdown: "## Building\n\nBuild it.\n\n```bash\nmage build:tptctl\n```\n\nInstall it.\n\n```bash\nmage install:tptctl\n```\n",
			want:     []string{},
		},
		{
			name:     "markdown syntax inside a fence is read as command text",
			markdown: "```bash\n#### print the section\n- an argument, not a list item\n```\n",
			want:     []string{},
		},
		{
			name:     "link text naming no destination is reported",
			markdown: "The config is described [here](threeport-config.md) and [this](testing.md).\n",
			want: []string{
				`page.md:1: link text: link text "here" names no destination`,
				`page.md:1: link text: link text "this" names no destination`,
			},
		},
		{
			name:     "link text naming its destination passes",
			markdown: "The config is described in the [Threeport config doc](threeport-config.md).\n",
			want:     []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			problems := checkProse("page.md", test.markdown)

			got := make([]string, 0, len(problems))
			for _, problem := range problems {
				got = append(got, problem.String())
			}

			if len(got) != len(test.want) {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Errorf("problem %d: got %q, want %q", index, got[index], test.want[index])
				}
			}
		})
	}
}

// TestCheckProseDirReadsEveryPageBeneathIt asserts that the walk reaches
// markdown at any depth and reads nothing else, so a page filed in a
// subdirectory is held to the same conventions as one at the top.
func TestCheckProseDirReadsEveryPageBeneathIt(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proposals"), 0755); err != nil {
		t.Fatalf("failed to create the test docs tree: %v", err)
	}

	pages := map[string]string{
		"index.md":               "#### Too Deep\n",
		"proposals/extension.md": "- a hyphen marker\n",
		"proposals/diagram.svg":  "#### not markdown\n",
		"proposals/notes.txt":    "- not markdown\n",
	}
	for name, contents := range pages {
		if err := os.WriteFile(filepath.Join(root, name), []byte(contents), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", name, err)
		}
	}

	problems, err := checkProseDir(root)
	if err != nil {
		t.Fatalf("failed to check the test docs tree: %v", err)
	}

	want := []string{
		filepath.Join(root, "index.md") + ":1: heading depth: 4 levels deep, go no deeper than 3",
		filepath.Join(root, "proposals", "extension.md") + ":1: bullet marker: list item opens with -, use *",
	}
	if len(problems) != len(want) {
		t.Fatalf("got %v, want %q", problems, want)
	}
	for index := range want {
		if problems[index].String() != want[index] {
			t.Errorf("problem %d: got %q, want %q", index, problems[index].String(), want[index])
		}
	}
}

// TestProseScopeCoversTheMeasuredTreesOnly asserts that the check stays on the
// two trees the conventions were measured from. The published site follows the
// documentation theme's conventions instead, so adding it here reports hundreds
// of findings that are not defects.
func TestProseScopeCoversTheMeasuredTreesOnly(t *testing.T) {
	want := []string{"docs/dev", "docs/design"}

	if len(proseDirs) != len(want) {
		t.Fatalf("got %q, want %q", proseDirs, want)
	}
	for index := range want {
		if proseDirs[index] != want[index] {
			t.Errorf("directory %d: got %q, want %q", index, proseDirs[index], want[index])
		}
	}
}

// TestReadRunnableLinesReadsOnlyShellBlocks asserts that the fence reader
// returns the contents of shell and unlabelled blocks and nothing else. The
// closing-fence case is the one that matters: a closing marker carries no
// language, so a reader that tracks only one piece of state treats the line
// ending a Go block as the line starting a shell block and returns prose.
func TestReadRunnableLinesReadsOnlyShellBlocks(t *testing.T) {
	tests := []struct {
		name     string
		markdown string
		want     []string
	}{
		{
			name:     "unlabelled block is read",
			markdown: "text\n```\ntptctl get workloads\n```\nmore text\n",
			want:     []string{"tptctl get workloads"},
		},
		{
			name:     "bash block is read",
			markdown: "```bash\nmage dev:up\n```\n",
			want:     []string{"mage dev:up"},
		},
		{
			name:     "go block is skipped",
			markdown: "```go\n// make domain name attachment\n```\n",
			want:     []string{},
		},
		{
			name:     "yaml block is skipped",
			markdown: "```yaml\nname: make sure\n```\n",
			want:     []string{},
		},
		{
			name:     "a go block does not leak into what follows it",
			markdown: "```go\n// make AWS attachment\n```\n\nplain prose that mentions make sense\n",
			want:     []string{},
		},
		{
			name:     "a skipped block followed by a shell block reads only the shell block",
			markdown: "```yaml\nkey: value\n```\n\n```bash\ntptctl up\n```\n",
			want:     []string{"tptctl up"},
		},
		{
			name:     "indented fences are recognized",
			markdown: "1. step\n\n    ```bash\n    mage build:tptctl\n    ```\n",
			want:     []string{"    mage build:tptctl"},
		},
		{
			name:     "prose outside any fence is skipped",
			markdown: "Run make sure you have tptctl installed first.\n",
			want:     []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "page.md")
			if err := os.WriteFile(path, []byte(test.markdown), 0644); err != nil {
				t.Fatalf("failed to write the test page: %v", err)
			}

			got, err := readRunnableLines(path)
			if err != nil {
				t.Fatalf("failed to read runnable lines: %v", err)
			}

			if len(got) != len(test.want) {
				t.Fatalf("got %d lines %q, want %d lines %q", len(got), got, len(test.want), test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Errorf("line %d: got %q, want %q", index, got[index], test.want[index])
				}
			}
		})
	}
}

// TestCheckTargetsRejectsUndeclaredTargets asserts that a documented build
// target is matched against the build system case-insensitively, that an
// allowed target is exempt, and that each missing target is reported once no
// matter how often the documentation names it.
func TestCheckTargetsRejectsUndeclaredTargets(t *testing.T) {
	declared := []string{"Build:Tptctl", "Dev:Up", "Test:Integration"}
	allowed := []string{"build:plugin"}

	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "a declared target passes whatever its casing",
			lines: []string{"mage build:tptctl", "mage Dev:Up"},
			want:  []string{},
		},
		{
			name:  "an undeclared target is reported",
			lines: []string{"mage buildTptdev"},
			want:  []string{"mage buildTptdev: no such target"},
		},
		{
			name:  "an allowed target is exempt",
			lines: []string{"mage build:plugin"},
			want:  []string{},
		},
		{
			name:  "a repeated undeclared target is reported once",
			lines: []string{"mage createLocalRegistry", "mage createLocalRegistry"},
			want:  []string{"mage createLocalRegistry: no such target"},
		},
		{
			name:  "a target named mid-sentence is not read as an invocation",
			lines: []string{"you can make sure the mage targets are current"},
			want:  []string{},
		},
		{
			name:  "a shell prompt before the command is tolerated",
			lines: []string{"$ mage dev:up"},
			want:  []string{},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := checkTargets(test.lines, mageCall, "mage", declared, allowed)

			if len(got) != len(test.want) {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Errorf("problem %d: got %q, want %q", index, got[index], test.want[index])
				}
			}
		})
	}
}

// TestCheckSampleFilesFindsRenamedSamples asserts that a download URL is
// resolved against the tree rather than the network, so a sample renamed in
// this repository is reported even while the URL's branch still serves the old
// path.
func TestCheckSampleFilesFindsRenamedSamples(t *testing.T) {
	repoRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repoRoot, "samples", "helm"), 0755); err != nil {
		t.Fatalf("failed to create the test samples tree: %v", err)
	}
	present := filepath.Join(repoRoot, "samples", "helm", "wordpress.yaml")
	if err := os.WriteFile(present, []byte("{}\n"), 0644); err != nil {
		t.Fatalf("failed to write the test sample: %v", err)
	}

	tests := []struct {
		name  string
		lines []string
		want  []string
	}{
		{
			name:  "a sample this repository carries passes",
			lines: []string{"curl -O https://raw.githubusercontent.com/threeport/threeport/main/samples/helm/wordpress.yaml"},
			want:  []string{},
		},
		{
			name:  "a sample renamed away is reported",
			lines: []string{"curl -O https://raw.githubusercontent.com/threeport/threeport/main/samples/workload/gone.yaml"},
			want:  []string{"samples/workload/gone.yaml: no such sample in this repository"},
		},
		{
			name:  "the branch in the URL does not matter",
			lines: []string{"curl -O https://raw.githubusercontent.com/threeport/threeport/0.7/samples/helm/wordpress.yaml"},
			want:  []string{},
		},
		{
			name:  "a different repository is not checked against this tree",
			lines: []string{"curl -O https://raw.githubusercontent.com/threeport/releases/main/samples/k8s-runtime.yaml"},
			want:  []string{},
		},
		{
			name: "a repeated missing sample is reported once",
			lines: []string{
				"curl -O https://raw.githubusercontent.com/threeport/threeport/main/samples/workload/gone.yaml",
				"curl -O https://raw.githubusercontent.com/threeport/threeport/main/samples/workload/gone.yaml",
			},
			want: []string{"samples/workload/gone.yaml: no such sample in this repository"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := checkSampleFiles(test.lines, repoRoot)

			if len(got) != len(test.want) {
				t.Fatalf("got %q, want %q", got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Errorf("problem %d: got %q, want %q", index, got[index], test.want[index])
				}
			}
		})
	}
}

// TestReadMakefileTargetsReadsDeclarationsOnly asserts that target names come
// from declaration lines and that recipe lines and comments are ignored.
func TestReadMakefileTargetsReadsDeclarationsOnly(t *testing.T) {
	makefile := "# a comment: not a target\n" +
		"dev-up:\n" +
		"\techo building: not a target\n" +
		"dev-down: dev-up\n" +
		"\techo done\n"

	path := filepath.Join(t.TempDir(), "Makefile")
	if err := os.WriteFile(path, []byte(makefile), 0644); err != nil {
		t.Fatalf("failed to write the test Makefile: %v", err)
	}

	got, err := readMakefileTargets(path)
	if err != nil {
		t.Fatalf("failed to read Makefile targets: %v", err)
	}

	want := []string{"dev-up", "dev-down"}
	if len(got) != len(want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	for index := range got {
		if got[index] != want[index] {
			t.Errorf("target %d: got %q, want %q", index, got[index], want[index])
		}
	}
}

// TestSpliceAgentsIndexWritesBetweenTheMarkers asserts that the documentation
// map lands between the marker lines and nowhere else, that the markers survive
// so the next run has somewhere to write, and that writing the same map a
// second time leaves the file byte for byte as it was. Without that last
// property every regeneration would show up as a change to the agent
// instructions whether or not the site navigation moved.
func TestSpliceAgentsIndexWritesBetweenTheMarkers(t *testing.T) {
	prologue := "# Instructions\n\n"
	epilogue := "\n\n# Architecture\n"
	instructions := prologue +
		agentsIndexBeginMarker + "\nan out of date map\n" + agentsIndexEndMarker +
		epilogue

	path := filepath.Join(t.TempDir(), "AGENTS.md")
	if err := os.WriteFile(path, []byte(instructions), 0644); err != nil {
		t.Fatalf("failed to write the test instructions: %v", err)
	}

	block := "\n# Documentation map\n\n| Topic | Where |\n|---|---|\n| SDK | `docs/docs/sdk/` |\n"
	if err := spliceAgentsIndex(path, block); err != nil {
		t.Fatalf("failed to splice the documentation map: %v", err)
	}

	spliced, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read the spliced instructions: %v", err)
	}

	want := prologue +
		agentsIndexBeginMarker + "\n" + block + "\n" + agentsIndexEndMarker +
		epilogue
	if string(spliced) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", spliced, want)
	}

	if err := spliceAgentsIndex(path, block); err != nil {
		t.Fatalf("failed to splice the documentation map a second time: %v", err)
	}

	respliced, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read the respliced instructions: %v", err)
	}
	if string(respliced) != string(spliced) {
		t.Errorf("a second splice changed the file, got:\n%s\nwant:\n%s", respliced, spliced)
	}
}

// TestSpliceAgentsIndexRejectsBrokenMarkers asserts that a file whose markers
// have been lost, reordered, or duplicated is reported. Writing nothing and
// reporting success would leave the map frozen at whatever it last said, which
// is the failure this check exists to prevent.
func TestSpliceAgentsIndexRejectsBrokenMarkers(t *testing.T) {
	tests := []struct {
		name         string
		instructions string
	}{
		{
			name:         "no markers at all",
			instructions: "# Instructions\n\nno map here\n",
		},
		{
			name:         "only the begin marker",
			instructions: agentsIndexBeginMarker + "\na map with no end\n",
		},
		{
			name:         "only the end marker",
			instructions: "a map with no beginning\n" + agentsIndexEndMarker + "\n",
		},
		{
			name:         "the markers the wrong way round",
			instructions: agentsIndexEndMarker + "\nprose\n" + agentsIndexBeginMarker + "\n",
		},
		{
			name: "the begin marker twice",
			instructions: agentsIndexBeginMarker + "\nfirst\n" +
				agentsIndexBeginMarker + "\nsecond\n" + agentsIndexEndMarker + "\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "AGENTS.md")
			if err := os.WriteFile(path, []byte(test.instructions), 0644); err != nil {
				t.Fatalf("failed to write the test instructions: %v", err)
			}

			if err := spliceAgentsIndex(path, "\n# Documentation map\n"); err == nil {
				t.Fatal("got no error, want a report that the markers cannot be used")
			}

			unchanged, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("failed to read the instructions back: %v", err)
			}
			if string(unchanged) != test.instructions {
				t.Errorf("the file was rewritten despite the error, got:\n%s", unchanged)
			}
		})
	}
}

// TestCollectPagePathsReachesEveryNestingDepth asserts that pages are found
// under a nested navigation section, which is how the object and runtime
// sections of the site are arranged.
func TestCollectPagePathsReachesEveryNestingDepth(t *testing.T) {
	navigation := `
nav:
  - Intro: 'index.md'
  - Objects:
    - Workloads:
      - 'Introduction': 'workloads/workload-intro.md'
      - 'Local': 'workloads/deploy-workload-local.md'
    - AWS:
      - 'Introduction': 'aws/aws-intro.md'
`

	path := filepath.Join(t.TempDir(), "mkdocs.yml")
	if err := os.WriteFile(path, []byte(navigation), 0644); err != nil {
		t.Fatalf("failed to write the test site config: %v", err)
	}

	sections, err := readSiteNavigation(path)
	if err != nil {
		t.Fatalf("failed to read the site navigation: %v", err)
	}

	if len(sections) != 2 {
		t.Fatalf("got %d sections, want 2", len(sections))
	}
	if sections[0].Title != "Intro" || sections[0].Locations[0] != "docs/docs/index.md" {
		t.Errorf("got root page section %+v, want Intro at docs/docs/index.md", sections[0])
	}

	want := []string{"docs/docs/workloads/", "docs/docs/aws/"}
	if len(sections[1].Locations) != len(want) {
		t.Fatalf("got %q, want %q", sections[1].Locations, want)
	}
	for index := range want {
		if sections[1].Locations[index] != want[index] {
			t.Errorf("location %d: got %q, want %q", index, sections[1].Locations[index], want[index])
		}
	}
}
