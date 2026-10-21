package swift

import (
	"strings"

	"github.com/git-pkgs/manifests/internal/core"
)

const pluginKind = "plugin"

type targetDeclaration struct {
	kind string
	args []argument
}

// Targets may reference a package by its identity or by its declared name.
func bindPackage(bindings map[string]string, args []argument, identity, name string) {
	for _, alias := range []string{identity, argumentString(args, "name")} {
		if alias == "" {
			continue
		}
		alias = strings.ToLower(alias)
		if previous, exists := bindings[alias]; exists && previous != name {
			bindings[alias] = ""
		} else {
			bindings[alias] = name
		}
	}
}

func applyTargetScopes(deps []core.Dependency, bindings map[string]string, args []argument) {
	elements, ok := arrayArguments(argumentValue(args, "targets"))
	if !ok {
		return
	}
	targets := make([]targetDeclaration, 0, len(elements))
	localNames := make(map[string]bool)
	for _, element := range elements {
		kind, targetArgs, valid := memberCall(element.value)
		if !valid || !targetKind(kind) {
			return
		}
		name := argumentString(targetArgs, "name")
		if name == "" {
			return
		}
		targets = append(targets, targetDeclaration{kind: kind, args: targetArgs})
		localNames[name] = true
	}
	uses := make(map[string]core.Scope)
	for _, target := range targets {
		scope := core.Runtime
		switch target.kind {
		case "testTarget":
			scope = core.Test
		case pluginKind, "macro":
			scope = core.Build
		}
		if !recordUses(uses, bindings, localNames, argumentValue(target.args, "dependencies"), scope) ||
			!recordUses(uses, bindings, localNames, argumentValue(target.args, "plugins"), core.Build) {
			// Unqualified products may belong to any declared package.
			return
		}
	}
	for i := range deps {
		if scope, found := uses[deps[i].Name]; found {
			deps[i].Scope = scope
		}
	}
}

func targetKind(kind string) bool {
	switch kind {
	case "target", "executableTarget", "testTarget", pluginKind, "macro", "systemLibrary", "binaryTarget":
		return true
	default:
		return false
	}
}

func recordUses(uses map[string]core.Scope, bindings map[string]string, locals map[string]bool, tokens []token, scope core.Scope) bool {
	if len(tokens) == 0 {
		return true
	}
	elements, ok := arrayArguments(tokens)
	if !ok {
		return false
	}
	for _, element := range elements {
		if name := literalValue(element.value); name != "" && locals[name] {
			continue
		}
		kind, args, valid := memberCall(element.value)
		if !valid {
			return false
		}
		switch kind {
		case "target", "byName":
			if !locals[argumentString(args, "name")] {
				return false
			}
		case "product", pluginKind:
			identity := argumentString(args, "package")
			if kind == pluginKind && identity == "" && locals[argumentString(args, "name")] {
				continue
			}
			name := bindings[strings.ToLower(identity)]
			if name == "" {
				return false
			}
			uses[name] = mergeScope(uses[name], scope)
		default:
			return false
		}
	}
	return true
}

func mergeScope(previous, next core.Scope) core.Scope {
	if previous == core.Runtime || next == core.Runtime {
		return core.Runtime
	}
	if previous == core.Build || next == core.Build {
		return core.Build
	}
	return next
}

func arrayArguments(tokens []token) ([]argument, bool) {
	args, end := delimitedArguments(tokens, "[", "]")
	return args, end > 0 && end == len(tokens)
}

func memberCall(tokens []token) (string, []argument, bool) {
	if len(tokens) < 2 || !tokens[0].is(".") || tokens[1].literal {
		return "", nil, false
	}
	args, end := delimitedArguments(tokens[2:], "(", ")")
	return tokens[1].text, args, end > 0 && end == len(tokens)-2
}
