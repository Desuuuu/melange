package parser

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/openfga/language/pkg/go/transformer"

	"github.com/pthm/melange/pkg/schema"
)

// manifestData holds the parsed manifest and the module files it references.
type manifestData struct {
	Raw           []byte                   // Original manifest bytes
	SchemaVersion string                   // Schema version from the manifest
	Modules       []transformer.ModuleFile // Module files in manifest order
}

// readManifest reads an fga.mod manifest and all referenced module files.
// Shared by ParseModularSchema (which compiles modules) and ReadManifestContents
// (which concatenates raw contents for hashing).
func readManifest(manifestPath string) (*manifestData, error) {
	raw, err := os.ReadFile(manifestPath) //nolint:gosec // path is from trusted source
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	modFile, err := transformer.TransformModFile(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	baseDir := filepath.Dir(manifestPath)
	modules := make([]transformer.ModuleFile, 0, len(modFile.Contents.Value))

	for _, entry := range modFile.Contents.Value {
		fullPath := filepath.Join(baseDir, entry.Value)
		content, err := os.ReadFile(fullPath) //nolint:gosec // path is from trusted source
		if err != nil {
			return nil, fmt.Errorf("reading module %s: %w", entry.Value, err)
		}
		modules = append(modules, transformer.ModuleFile{
			Name:     entry.Value,
			Contents: string(content),
		})
	}

	return &manifestData{
		Raw:           raw,
		SchemaVersion: modFile.Schema.Value,
		Modules:       modules,
	}, nil
}

// ParseModularSchemaFromStrings parses pre-read module contents and merges
// them into unified type definitions. Useful for testing and embedded schemas
// where module files are already in memory.
func ParseModularSchemaFromStrings(modules map[string]string, schemaVersion string) ([]schema.TypeDefinition, error) {
	moduleFiles := make([]transformer.ModuleFile, 0, len(modules))
	for name, content := range modules {
		moduleFiles = append(moduleFiles, transformer.ModuleFile{
			Name:     name,
			Contents: content,
		})
	}

	model, err := transformer.TransformModuleFilesToModel(moduleFiles, schemaVersion)
	if err != nil {
		return nil, fmt.Errorf("compiling modules: %w", err)
	}

	return convertModel(model), nil
}

// ReadManifestContents reads an fga.mod manifest and all referenced module
// files, returning their concatenated contents as a single byte slice.
// The output is deterministic (files ordered as listed in the manifest)
// and suitable for content hashing in migration skip detection.
func ReadManifestContents(manifestPath string) ([]byte, error) {
	data, err := readManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	buf.Write(data.Raw)

	for _, mod := range data.Modules {
		buf.WriteString("\n---\n")
		buf.WriteString(mod.Name)
		buf.WriteString("\n")
		buf.WriteString(mod.Contents)
	}

	return buf.Bytes(), nil
}
