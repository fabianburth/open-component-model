package transformation

import (
	"context"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v1"
	dir "ocm.software/open-component-model/bindings/go/input/dir"
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

	method, err := dir.NewInputMethod(spec.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("failed creating dir input method: %w", err)
	}

	result, err := method.ProcessResource(ctx, &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: spec.Resource.Input,
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed processing dir input: %w", err)
	}

	// Determine output path
	outputPath, err := determineOutputPath(spec.OutputPath, "dir-input")
	if err != nil {
		return nil, fmt.Errorf("failed determining output path: %w", err)
	}

	// Buffer blob to file spec
	fileSpec, err := filesystem.BlobToSpec(result.ProcessedBlobData, outputPath)
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
