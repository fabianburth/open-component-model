package transformation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/spec/v1"
	dir "ocm.software/open-component-model/bindings/go/input/dir"
	dirv1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
	"ocm.software/open-component-model/bindings/go/input/dir/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// DirInput is a transformer that reads a directory from the local filesystem
// and buffers it to an output file for downstream consumption.
type DirInput struct {
	Scheme *runtime.Scheme
}

func (t *DirInput) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.DirInput
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to dir input transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for dir input transformation")
	}

	spec := transformation.Spec

	if spec.Resource == nil || spec.Resource.Input == nil {
		return nil, fmt.Errorf("resource with input is required for dir input transformation")
	}

	// Deserialize input-specific attributes from Resource.Input
	var v1Dir dirv1.Dir
	if err := json.Unmarshal(spec.Resource.Input.Data, &v1Dir); err != nil {
		return nil, fmt.Errorf("failed deserializing dir input from resource: %w", err)
	}

	if v1Dir.Path == "" {
		return nil, fmt.Errorf("path is required for dir input transformation")
	}

	// Resolve relative paths against the working directory.
	// GetV1DirBlob uses GetBlobFromPath which expects an absolute path for os.Stat.
	dirPath := v1Dir.Path
	if !filepath.IsAbs(dirPath) && spec.WorkingDirectory != "" {
		dirPath = filepath.Join(spec.WorkingDirectory, dirPath)
	}
	v1Dir.Path = dirPath

	blob, err := dir.GetV1DirBlob(ctx, v1Dir, spec.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed getting dir blob: %w", err)
	}

	// Determine output path
	outputPath, err := determineOutputPath(spec.OutputPath, "dir-input")
	if err != nil {
		return nil, fmt.Errorf("failed determining output path: %w", err)
	}

	// Buffer blob to file spec
	fileSpec, err := filesystem.BlobToSpec(blob, outputPath)
	if err != nil {
		return nil, fmt.Errorf("failed buffering blob to file: %w", err)
	}

	// Populate output
	if transformation.Output == nil {
		transformation.Output = &v1alpha1.DirInputOutput{}
	}
	transformation.Output.File = *fileSpec
	transformation.Output.Resource = constructorv1.ConvertResourceToV2(spec.Resource)

	return &transformation, nil
}

// determineOutputPath determines the output path for buffering the blob content.
// If the outputPath is empty, it creates a temporary file in the default temp directory.
// If the outputPath is provided and is a directory, it creates a temporary file in that directory.
func determineOutputPath(outputPath string, filePrefix string) (string, error) {
	if outputPath == "" {
		tempFile, err := os.CreateTemp("", filePrefix+"-*")
		if err != nil {
			return "", fmt.Errorf("failed creating temporary file: %w", err)
		}
		_ = tempFile.Close()
		return tempFile.Name(), nil
	}

	info, err := os.Stat(outputPath)
	if err != nil {
		return "", fmt.Errorf("output path does not exist: %w", err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("output path %q is a file, not a directory", outputPath)
	}

	tmpFile, err := os.CreateTemp(outputPath, filePrefix+"-*")
	if err != nil {
		return "", fmt.Errorf("failed creating temporary file in output directory: %w", err)
	}
	_ = tmpFile.Close()
	return tmpFile.Name(), nil
}
