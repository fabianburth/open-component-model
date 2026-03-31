package transformation

import (
	"context"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	file "ocm.software/open-component-model/bindings/go/input/file"
	filev1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/input/file/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// FileInput is a transformer that reads a file from the local filesystem
// and buffers it to an output file for downstream consumption.
type FileInput struct {
	Scheme *runtime.Scheme
}

func (t *FileInput) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.FileInput
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to file input transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for file input transformation")
	}

	spec := transformation.Spec

	if spec.Path == "" {
		return nil, fmt.Errorf("path is required for file input transformation")
	}

	// Convert to v1.File spec for GetV1FileBlob
	v1File := filev1.File{
		Path:      spec.Path,
		MediaType: spec.MediaType,
		Compress:  spec.Compress,
	}

	blob, err := file.GetV1FileBlob(v1File, spec.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed getting file blob: %w", err)
	}

	// Determine output path
	outputPath, err := determineOutputPath(spec.OutputPath, "file-input")
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
		transformation.Output = &v1alpha1.FileInputOutput{}
	}
	transformation.Output.File = *fileSpec

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
