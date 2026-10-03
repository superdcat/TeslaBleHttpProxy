package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const publishWorkflow = ".github/workflows/release.yml"

func readRepoFile(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// meaningfulLines returns the trimmed lines of src, without blank lines and # comments.
func meaningfulLines(src string) []string {
	var lines []string
	for _, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// dockerStages splits a Dockerfile into stages, each starting with its FROM line.
func dockerStages(src string) [][]string {
	var stages [][]string
	for _, line := range meaningfulLines(src) {
		if strings.HasPrefix(line, "FROM ") {
			stages = append(stages, nil)
		}
		if len(stages) > 0 {
			stages[len(stages)-1] = append(stages[len(stages)-1], line)
		}
	}
	return stages
}

func contains(lines []string, want string) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

func TestDockerImageRuntimeContract(t *testing.T) {
	stages := dockerStages(readRepoFile(t, "Dockerfile"))
	if len(stages) == 0 {
		t.Fatalf("Dockerfile has no FROM stage")
	}
	if len(stages) < 2 {
		t.Fatalf("expected a builder stage and a runtime stage, got %d stage(s)", len(stages))
	}
	final := stages[len(stages)-1]
	if final[0] != "FROM scratch" {
		t.Errorf("runtime stage = %q, want FROM scratch", final[0])
	}
	for _, want := range []string{
		"ENV TZ=Europe/Berlin",
		"COPY --from=builder /go/bin/teslaBleHttpProxy /teslaBleHttpProxy",
		"COPY --from=builder /go/bin/key /key",
		"EXPOSE 8080",
		`ENTRYPOINT ["/teslaBleHttpProxy"]`,
	} {
		if !contains(final, want) {
			t.Errorf("runtime stage misses %q (wimaha 2.3.0 image contract)", want)
		}
	}
	for _, line := range final {
		for _, forbidden := range []string{"USER ", "WORKDIR ", "VOLUME ", "CMD ", "RUN "} {
			if strings.HasPrefix(strings.ToUpper(line), forbidden) {
				t.Errorf("runtime stage must not contain %q: it would change the wimaha 2.3.0 image contract", line)
			}
		}
	}
}

func TestDockerBuildTakesVersionFromBuildArg(t *testing.T) {
	stages := dockerStages(readRepoFile(t, "Dockerfile"))
	if len(stages) == 0 {
		t.Fatalf("Dockerfile has no FROM stage")
	}
	builder := stages[0]
	if !strings.HasPrefix(builder[0], "FROM --platform=${BUILDPLATFORM} golang:1.25.") {
		t.Errorf("builder stage = %q, want a golang:1.25.x image on the build platform (cross-compilation, no QEMU)", builder[0])
	}
	for _, want := range []string{"ARG TARGETVARIANT", "ARG GOARM=${TARGETVARIANT#v}", "ARG VERSION"} {
		if !contains(builder, want) {
			t.Errorf("builder stage misses %q", want)
		}
	}
	argIdx, runIdx := -1, -1
	for i, line := range builder {
		if line == "ARG VERSION" && argIdx < 0 {
			argIdx = i
		}
		if strings.HasPrefix(line, "RUN ") && strings.Contains(line, "GOARM=${GOARM}") && strings.HasSuffix(line, `make build-docker VERSION="${VERSION}"`) {
			runIdx = i
		}
	}
	if runIdx < 0 {
		t.Error(`builder stage must build with GOARM=${GOARM} and make build-docker VERSION="${VERSION}"`)
	} else if argIdx < 0 || argIdx > runIdx {
		t.Errorf("builder stage: ARG VERSION (line index %d) must precede the make build-docker RUN (line index %d)", argIdx, runIdx)
	}
}

func TestDockerignoreKeepsVersionOutOfGit(t *testing.T) {
	entries := meaningfulLines(readRepoFile(t, ".dockerignore"))
	for _, want := range []string{".git", "key/", "docs/"} {
		if !contains(entries, want) {
			t.Errorf(".dockerignore misses %q", want)
		}
	}
	needed := map[string]bool{"go.mod": true, "go.sum": true, "main.go": true, "Makefile": true, "config": true, "internal": true, "html": true, "static": true, "LICENSE": true, "NOTICE": true}
	for _, entry := range entries {
		name := strings.Trim(entry, "/")
		if needed[name] || name == "*" || name == "**" {
			t.Errorf(".dockerignore entry %q excludes a build input", entry)
		}
	}
}

func TestPublishWorkflowUsesOnlyGitHubToken(t *testing.T) {
	src := readRepoFile(t, publishWorkflow)
	for _, m := range regexp.MustCompile(`secrets\.([A-Za-z0-9_]+)`).FindAllStringSubmatch(src, -1) {
		if m[1] != "GITHUB_TOKEN" {
			t.Errorf("%s uses secrets.%s: only GITHUB_TOKEN is allowed", publishWorkflow, m[1])
		}
	}
	for _, file := range []string{publishWorkflow, "Makefile"} {
		content := readRepoFile(t, file)
		for _, banned := range []string{"docker.io", "DOCKER_USER", "DOCKER_PASS", "wimaha/tesla-ble-http-proxy", "push=true"} {
			if strings.Contains(content, banned) {
				t.Errorf("%s still references %q (Docker Hub publication)", file, banned)
			}
		}
	}
	if !strings.Contains(src, "IMAGE: ghcr.io/superdcat/tesla-ble-http-proxy") {
		t.Errorf("%s must publish ghcr.io/superdcat/tesla-ble-http-proxy", publishWorkflow)
	}
}

func TestPublishWorkflowTagRules(t *testing.T) {
	lines := meaningfulLines(readRepoFile(t, publishWorkflow))
	for _, want := range []string{
		`- "*-tb.*"`,
		"latest=false",
		"type=ref,event=tag",
		"type=raw,value=latest,enable=${{ github.ref_type == 'tag' }}",
		"type=sha,enable=${{ github.ref_type == 'tag' }}",
		"type=edge,branch=main",
	} {
		if !contains(lines, want) {
			t.Errorf("%s misses %q", publishWorkflow, want)
		}
	}
	latest := 0
	for _, line := range lines {
		if strings.Contains(line, "value=latest") {
			latest++
		}
	}
	if latest != 1 {
		t.Errorf("%s declares the latest tag %d times, want exactly once (tags only)", publishWorkflow, latest)
	}
	// Attestations would add unknown/unknown entries to the index (platform check is strict).
	builds, noProvenance, noSBOM := 0, 0, 0
	for _, line := range lines {
		if strings.Contains(line, "uses: docker/build-push-action") {
			builds++
		}
		if line == "provenance: false" {
			noProvenance++
		}
		if line == "sbom: false" {
			noSBOM++
		}
	}
	if builds == 0 || noProvenance != builds {
		t.Errorf("%s has %d build-push step(s) but %d \"provenance: false\"", publishWorkflow, builds, noProvenance)
	}
	if noSBOM != builds {
		t.Errorf("%s has %d build-push step(s) but %d \"sbom: false\"", publishWorkflow, builds, noSBOM)
	}
}

func TestWorkflowActionsPinnedToCommitSHA(t *testing.T) {
	files, err := filepath.Glob(".github/workflows/*.yml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no workflow found: %v", err)
	}
	usesRe := regexp.MustCompile(`^\s*(?:-\s+)?uses:\s*(\S+)\s*(.*)$`)
	pinnedRe := regexp.MustCompile(`^[^@\s]+@[0-9a-f]{40}$`)
	commentRe := regexp.MustCompile(`^#\s*v\d+\.\d+\.\d+$`)
	for _, file := range files {
		for i, line := range strings.Split(readRepoFile(t, file), "\n") {
			m := usesRe.FindStringSubmatch(line)
			if m == nil || strings.HasPrefix(m[1], "./") {
				continue
			}
			if !pinnedRe.MatchString(m[1]) {
				t.Errorf("%s:%d: %q is not pinned to a full commit SHA", file, i+1, m[1])
			}
			if !commentRe.MatchString(strings.TrimSpace(m[2])) {
				t.Errorf("%s:%d: %q lacks a trailing \"# vX.Y.Z\" comment", file, i+1, m[1])
			}
		}
	}
}
