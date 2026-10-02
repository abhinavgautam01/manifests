package crystal

import (
	"github.com/git-pkgs/manifests/internal/core"
	"strings"

	"go.yaml.in/yaml/v3"
)

func init() {
	core.Register("crystal", core.Manifest, &shardYMLParser{}, core.ExactMatch("shard.yml"))
	core.Register("crystal", core.Lockfile, &shardLockParser{}, core.ExactMatch("shard.lock"))
}

// extractShardName extracts shard name from "  name:" lines indented by exactly indent spaces.
func extractShardName(line string, indent int) (string, bool) {
	if indent == 0 || len(line) < indent+2 || core.LeadingSpaces(line) != indent {
		return "", false
	}
	if line[len(line)-1] != ':' {
		return "", false
	}
	return line[indent : len(line)-1], true
}

// extractShardValue extracts value from "    key: value" lines indented deeper than indent spaces.
func extractShardValue(line, prefix string, indent int) (string, bool) {
	if core.LeadingSpaces(line) <= indent {
		return "", false
	}
	return strings.CutPrefix(strings.TrimLeft(line, " "), prefix)
}

// shardYMLParser parses shard.yml files.
type shardYMLParser struct{}

type shardYML struct {
	Name                    string              `yaml:"name"`
	Version                 string              `yaml:"version"`
	Scripts                 map[string]any      `yaml:"scripts"`
	Dependencies            map[string]shardDep `yaml:"dependencies"`
	DevelopmentDependencies map[string]shardDep `yaml:"development_dependencies"`
}

type shardDep struct {
	GitHub  string `yaml:"github"`
	GitLab  string `yaml:"gitlab"`
	Git     string `yaml:"git"`
	Path    string `yaml:"path"`
	Version string `yaml:"version"`
	Branch  string `yaml:"branch"`
	Tag     string `yaml:"tag"`
	Commit  string `yaml:"commit"`
}

func (p *shardYMLParser) Parse(filename string, content []byte) (*core.Result, error) {
	var shard shardYML
	if err := yaml.Unmarshal(content, &shard); err != nil {
		return nil, &core.ParseError{Filename: filename, Err: err}
	}

	var deps []core.Dependency

	for name, dep := range shard.Dependencies {
		deps = append(deps, core.Dependency{
			Name:    name,
			Version: getShardVersion(dep),
			Scope:   core.Runtime,
			Direct:  true,
		})
	}

	for name, dep := range shard.DevelopmentDependencies {
		deps = append(deps, core.Dependency{
			Name:    name,
			Version: getShardVersion(dep),
			Scope:   core.Development,
			Direct:  true,
		})
	}

	return &core.Result{Name: shard.Name, Version: shard.Version, Dependencies: deps, Scripts: core.StringScripts(shard.Scripts)}, nil
}

func getShardVersion(dep shardDep) string {
	if dep.Version != "" {
		return dep.Version
	}
	if dep.Tag != "" {
		return dep.Tag
	}
	if dep.Branch != "" {
		return dep.Branch
	}
	return ""
}

// shardLockParser parses shard.lock files using regex for speed.
type shardLockParser struct{}

func (p *shardLockParser) Parse(filename string, content []byte) (*core.Result, error) {
	text := string(content)
	deps := make([]core.Dependency, 0, core.EstimateDeps(len(content)))

	inShards := false
	indent := 0
	var currentName string
	var currentVersion string

	core.ForEachLine(text, func(line string) bool {
		// Detect shards: section
		if line == "shards:" {
			inShards = true
			indent = 0
			return true
		}

		// Comment-only lines carry no data and must not set the indent
		if !inShards || core.IsYAMLComment(line) {
			return true
		}

		// The first indented line sets the indent width of shard names
		if indent == 0 && strings.TrimSpace(line) != "" {
			indent = core.LeadingSpaces(line)
		}

		// Shard name
		if name, ok := extractShardName(line, indent); ok {
			// Save previous shard if any
			if currentName != "" {
				deps = append(deps, core.Dependency{
					Name:    currentName,
					Version: currentVersion,
					Scope:   core.Runtime,
					Direct:  false,
				})
			}
			currentName = name
			currentVersion = ""
			return true
		}

		// Version or commit, nested under the shard name
		if currentName != "" {
			if v, ok := extractShardValue(line, "version: ", indent); ok {
				currentVersion = v
			} else if v, ok := extractShardValue(line, "commit: ", indent); ok {
				if currentVersion == "" { // version takes precedence
					currentVersion = v
				}
			}
		}
		return true
	})

	// Don't forget the last shard
	if currentName != "" {
		deps = append(deps, core.Dependency{
			Name:    currentName,
			Version: currentVersion,
			Scope:   core.Runtime,
			Direct:  false,
		})
	}

	return &core.Result{Dependencies: deps}, nil
}
