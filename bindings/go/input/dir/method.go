package dir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/dir/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var ErrDirsDoNotRequireCredentials = fmt.Errorf("directories do not require credentials")

var _ interface {
	constructorruntime.ResourceInputMethod
	constructorruntime.SourceInputMethod
} = (*InputMethod)(nil)

// InputMethod processes directory input specifications into blobs.
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
	return nil, ErrDirsDoNotRequireCredentials
}

func (i *InputMethod) ProcessResource(ctx context.Context, resource *constructorruntime.Resource, _ map[string]string) (*constructorruntime.ResourceInputMethodResult, error) {
	var dir v1.Dir
	if err := Scheme.Convert(resource.Input, &dir); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	resolveRelativeDirPath(&dir, i.WorkingDirectory)

	dirBlob, err := GetV1DirBlob(ctx, dir, i.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error getting dir blob based on resource input specification: %w", err)
	}

	return &constructorruntime.ResourceInputMethodResult{ProcessedBlobData: dirBlob}, nil
}

func (i *InputMethod) GetSourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Source) (runtime.Identity, error) {
	return nil, ErrDirsDoNotRequireCredentials
}

func (i *InputMethod) ProcessSource(ctx context.Context, src *constructorruntime.Source, _ map[string]string) (*constructorruntime.SourceInputMethodResult, error) {
	var dir v1.Dir
	if err := Scheme.Convert(src.Input, &dir); err != nil {
		return nil, fmt.Errorf("error converting source input spec: %w", err)
	}

	resolveRelativeDirPath(&dir, i.WorkingDirectory)

	dirBlob, err := GetV1DirBlob(ctx, dir, i.WorkingDirectory)
	if err != nil {
		return nil, fmt.Errorf("error getting dir blob based on source input specification: %w", err)
	}

	return &constructorruntime.SourceInputMethodResult{ProcessedBlobData: dirBlob}, nil
}

// resolveRelativeDirPath resolves a relative directory path against the working directory.
// GetV1DirBlob uses GetBlobFromPath which expects an absolute path for os.Stat.
func resolveRelativeDirPath(dir *v1.Dir, workingDirectory string) {
	if !filepath.IsAbs(dir.Path) && workingDirectory != "" {
		dir.Path = filepath.Join(workingDirectory, dir.Path)
	}
}
