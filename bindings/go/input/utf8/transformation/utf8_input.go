package transformation

import (
	"context"
	"fmt"
	"os"

	"ocm.software/open-component-model/bindings/go/blob/filesystem"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v1"
	utf8pkg "ocm.software/open-component-model/bindings/go/input/utf8"
	"ocm.software/open-component-model/bindings/go/input/utf8/transformation/spec/v1alpha1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

// UTF8Input is a transformer that produces a blob from inline UTF-8 content
// and buffers it to an output file for downstream consumption.
type UTF8Input struct {
	Scheme *runtime.Scheme
}

func (t *UTF8Input) Transform(ctx context.Context, step runtime.Typed) (runtime.Typed, error) {
	var transformation v1alpha1.UTF8Input
	if err := t.Scheme.Convert(step, &transformation); err != nil {
		return nil, fmt.Errorf("failed converting generic transformation to utf8 input transformation: %w", err)
	}
	if transformation.Spec == nil {
		return nil, fmt.Errorf("spec is required for utf8 input transformation")
	}

	spec := transformation.Spec

	if spec.Resource == nil || spec.Resource.Input == nil {
		return nil, fmt.Errorf("resource with input is required for utf8 input transformation")
	}

	method := &utf8pkg.InputMethod{}

	result, err := method.ProcessResource(ctx, &constructorruntime.Resource{
		AccessOrInput: constructorruntime.AccessOrInput{
			Input: spec.Resource.Input,
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("failed processing utf8 input: %w", err)
	}

	// Determine output path
	outputPath, err := determineOutputPath(spec.OutputPath, "utf8-input")
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
		transformation.Output = &v1alpha1.UTF8InputOutput{}
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
