// Command check-sdk-version detects occupied SDK versions before tagging GOWA.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

const (
	goRepo  = "aldinokemal/gowa-sdk-go"
	phpRepo = "aldinokemal/gowa-sdk-php"
)

var (
	client        = &http.Client{Timeout: 30 * time.Second}
	stableVersion = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	commitSHA     = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	path := "../docs/openapi.yaml"
	switch len(args) {
	case 0:
	case 1:
		path = args[0]
	default:
		return errors.New("usage: go run ./tools/check-sdk-version [openapi.yaml]")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var spec struct {
		Info struct{ Version string }
	}
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("read OpenAPI: %w", err)
	}
	version := spec.Info.Version
	if !stableVersion.MatchString(version) {
		return errors.New("expected a stable semver in OpenAPI info.version (for example, 9.7.0)")
	}

	fmt.Printf("Checking SDK version slots for OpenAPI %s\n", version)
	var failures []error
	for _, sdk := range []struct {
		name  string
		check func(string) (string, error)
	}{{"Go", checkGo}, {"Node", checkNode}, {"PHP", checkPHP}} {
		result, err := sdk.check(version)
		if err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", sdk.name, err))
			continue
		}
		fmt.Printf("%s: %s\n", sdk.name, result)
	}
	if len(failures) != 0 {
		return fmt.Errorf("SDK version preflight failed for OpenAPI %s. Resolve occupied versions or failed lookups before creating the GOWA tag.\n%w",
			version, errors.Join(failures...))
	}
	return nil
}

func get(url string, absent ...int) ([]byte, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if slices.Contains(absent, resp.StatusCode) {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func gitTagCommit(repo, tag string) (string, error) {
	ref := "refs/tags/" + tag
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "git", "ls-remote", "--exit-code", "--tags",
		"https://github.com/"+repo+".git", ref, ref+"^{}").CombinedOutput()
	if err != nil {
		var commandErr *exec.ExitError
		if errors.As(err, &commandErr) && commandErr.ExitCode() == 2 {
			return "", nil
		}
		return "", fmt.Errorf("look up %s %s: %w: %s", repo, tag, err, strings.TrimSpace(string(output)))
	}

	var commit, peeled string
	for line := range strings.Lines(string(output)) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		switch fields[1] {
		case ref:
			commit = fields[0]
		case ref + "^{}":
			peeled = fields[0]
		}
	}
	if !commitSHA.MatchString(commit) || (peeled != "" && !commitSHA.MatchString(peeled)) {
		return "", fmt.Errorf("%s: invalid SHA for %s", repo, tag)
	}
	if peeled != "" {
		return peeled, nil
	}
	return commit, nil
}

func hasLine(body string, expected string) bool {
	for line := range strings.Lines(body) {
		if strings.TrimSpace(line) == expected {
			return true
		}
	}
	return false
}

func checkGo(version string) (string, error) {
	tag := "v" + version
	major, _, _ := strings.Cut(version, ".")
	segment := ""
	paths := []string{"/GOWA-SOURCE.txt"}
	if major != "0" && major != "1" {
		segment = "/v" + major
		paths = append(paths, segment+"/GOWA-SOURCE.txt")
	}
	commit, err := gitTagCommit(goRepo, tag)
	if err != nil {
		return "", err
	}
	if commit != "" {
		for _, path := range paths {
			body, err := get("https://raw.githubusercontent.com/"+goRepo+"/"+commit+path, http.StatusNotFound)
			if err != nil {
				return "", err
			}
			if hasLine(string(body), "OpenAPI version: "+version) {
				return "already published for OpenAPI " + version, nil
			}
		}
		return "", fmt.Errorf("tag %s is occupied by an SDK without matching OpenAPI provenance", tag)
	}

	body, err := get("https://proxy.golang.org/github.com/"+goRepo+segment+"/@v/"+tag+".info",
		http.StatusNotFound, http.StatusGone)
	if err != nil {
		return "", err
	}
	if body != nil {
		return "", fmt.Errorf("%s is already cached by the Go module proxy without a matching Git tag", tag)
	}
	return "tag " + tag + " is available", nil
}

func checkNode(version string) (string, error) {
	body, err := get("https://registry.npmjs.org/@aldinokemal2104%2fgowa-sdk-node/"+version, http.StatusNotFound)
	if err != nil {
		return "", err
	}
	if body == nil {
		return "package version " + version + " is available", nil
	}

	var manifest struct {
		Version    string
		GOWASource struct{ OpenAPIVersion string }
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return "", fmt.Errorf("read npm metadata: %w", err)
	}
	if manifest.Version == version && manifest.GOWASource.OpenAPIVersion == version {
		return "already published for OpenAPI " + version, nil
	}
	return "", fmt.Errorf("npm version %s is occupied by an SDK without matching OpenAPI provenance", version)
}

func checkPHP(version string) (string, error) {
	tag := "v" + version
	commit, err := gitTagCommit(phpRepo, tag)
	if err != nil {
		return "", err
	}
	body, err := get("https://repo.packagist.org/p2/" + phpRepo + ".json")
	if err != nil {
		return "", err
	}
	var index struct {
		Packages map[string][]struct {
			Version string
			Source  struct{ Reference string }
		}
	}
	if err := json.Unmarshal(body, &index); err != nil {
		return "", fmt.Errorf("read Packagist index: %w", err)
	}
	versions, ok := index.Packages[phpRepo]
	if !ok {
		return "", errors.New("Packagist index is missing the SDK package")
	}
	for _, published := range versions {
		if strings.TrimPrefix(published.Version, "v") == version && (commit == "" || published.Source.Reference != commit) {
			return "", fmt.Errorf("Composer version %s is indexed without a matching SDK Git tag", version)
		}
	}
	if commit == "" {
		return "tag " + tag + " is available", nil
	}

	body, err = get("https://api.github.com/repos/" + phpRepo + "/git/commits/" + commit)
	if err != nil {
		return "", err
	}
	var metadata struct{ Message string }
	if err := json.Unmarshal(body, &metadata); err != nil {
		return "", fmt.Errorf("read PHP SDK commit: %w", err)
	}
	if hasLine(metadata.Message, "GOWA-OpenAPI-Version: "+version) {
		return "already tagged for OpenAPI " + version, nil
	}
	return "", fmt.Errorf("tag %s is occupied by an SDK without matching OpenAPI provenance", tag)
}
