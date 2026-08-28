package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	configtemplate "github.com/nimweo/pulse-agent/configs"
	"gopkg.in/yaml.v3"
)

// Migrate adds settings missing from an existing configuration without
// replacing values already chosen by the user.
func Migrate(path string) (bool, error) {
	return MergeTemplate(path, configtemplate.Example)
}

func MergeTemplate(path string, template []byte) (bool, error) {
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, fmt.Errorf("resolve configuration path %q: %w", path, err)
	}
	path = resolvedPath

	contents, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read configuration file %q: %w", path, err)
	}

	existing, err := decodeYAMLDocument(contents)
	if err != nil {
		return false, fmt.Errorf("decode configuration file %q: %w", path, err)
	}
	defaults, err := decodeYAMLDocument(template)
	if err != nil {
		return false, fmt.Errorf("decode configuration template: %w", err)
	}
	if !mergeYAMLMapping(existing.Content[0], defaults.Content[0]) {
		return false, nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return false, fmt.Errorf("inspect configuration file %q: %w", path, err)
	}

	var output bytes.Buffer
	encoder := yaml.NewEncoder(&output)
	encoder.SetIndent(2)
	if err := encoder.Encode(existing.Content[0]); err != nil {
		return false, fmt.Errorf("encode migrated configuration: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return false, fmt.Errorf("close configuration encoder: %w", err)
	}

	temporary, err := os.CreateTemp(filepath.Dir(path), ".config.yaml.*")
	if err != nil {
		return false, fmt.Errorf("create temporary configuration file: %w", err)
	}
	temporaryPath := temporary.Name()
	removeTemporary := true
	defer func() {
		if removeTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("preserve configuration permissions: %w", err)
	}
	if err := preserveOwner(temporaryPath, info); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("preserve configuration ownership: %w", err)
	}
	if _, err := temporary.Write(output.Bytes()); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("write migrated configuration: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return false, fmt.Errorf("sync migrated configuration: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return false, fmt.Errorf("close migrated configuration: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return false, fmt.Errorf("replace configuration file %q: %w", path, err)
	}
	removeTemporary = false

	return true, nil
}

func decodeYAMLDocument(contents []byte) (*yaml.Node, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return nil, err
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("configuration root must be a mapping")
	}
	return &document, nil
}

func mergeYAMLMapping(destination *yaml.Node, defaults *yaml.Node) bool {
	if destination.Kind != yaml.MappingNode || defaults.Kind != yaml.MappingNode {
		return false
	}

	destinationValues := make(map[string]*yaml.Node, len(destination.Content)/2)
	for index := 0; index+1 < len(destination.Content); index += 2 {
		destinationValues[destination.Content[index].Value] = destination.Content[index+1]
	}

	changed := false
	for index := 0; index+1 < len(defaults.Content); index += 2 {
		defaultKey := defaults.Content[index]
		defaultValue := defaults.Content[index+1]
		destinationValue, ok := destinationValues[defaultKey.Value]
		if !ok {
			destination.Content = append(
				destination.Content,
				cloneYAMLNode(defaultKey),
				cloneYAMLNode(defaultValue),
			)
			changed = true
			continue
		}
		if mergeYAMLMapping(destinationValue, defaultValue) {
			changed = true
		}
	}

	return changed
}

func cloneYAMLNode(source *yaml.Node) *yaml.Node {
	clone := *source
	clone.Content = make([]*yaml.Node, len(source.Content))
	for index, child := range source.Content {
		clone.Content[index] = cloneYAMLNode(child)
	}
	return &clone
}
