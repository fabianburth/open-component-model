package file

import (
	"context"
	"fmt"
	"os"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/file/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var ErrFilesDoNotRequireCredentials = fmt.Errorf("files do not require credentials")

var _ interface {
	constructorruntime.ResourceInputMethod
	constructorruntime.SourceInputMethod
} = (*InputMethod)(nil)

// InputMethod processes file input specifications into blobs.
type InputMethod struct {
	WorkingDirectory string
}

// NewInputMethod creates a new InputMethod with the given working directory.
// If workingDir is empty, the current working directory is used.
func NewInputMethod(workingDir string) (*InputMethod, error) {
	if workingDir == "" {
		var err error
		workingDir, err = os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("error getting current working directory: %w", err)
		}
	}
	return &InputMethod{WorkingDirectory: workingDir}, nil
}

func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Resource) (runtime.Identity, error) {
	return nil, ErrFilesDoNotRequireCredentials
}

func (i *InputMethod) ProcessResource(_ context.Context, resource *constructorruntime.Resource, _ map[string]string) (*constructorruntime.ResourceInputMethodResult, error) {
	var file v1.File
	if err := Scheme.Convert(resource.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file, i.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob based on resource input specification: %w", err)
	}

	return &constructorruntime.ResourceInputMethodResult{ProcessedBlobData: fileBlob}, nil
}

func (i *InputMethod) GetSourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Source) (runtime.Identity, error) {
	return nil, ErrFilesDoNotRequireCredentials
}

func (i *InputMethod) ProcessSource(_ context.Context, src *constructorruntime.Source, _ map[string]string) (*constructorruntime.SourceInputMethodResult, error) {
	var file v1.File
	if err := Scheme.Convert(src.Input, &file); err != nil {
		return nil, fmt.Errorf("error converting source input spec: %w", err)
	}

	fileBlob, err := GetV1FileBlob(file, i.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error getting file blob based on source input specification: %w", err)
	}

	return &constructorruntime.SourceInputMethodResult{ProcessedBlobData: fileBlob}, nil
}
