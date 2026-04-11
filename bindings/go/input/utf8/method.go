package utf8

import (
	"context"
	"fmt"

	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	v1 "ocm.software/open-component-model/bindings/go/input/utf8/spec/v1"
	"ocm.software/open-component-model/bindings/go/runtime"
)

var ErrUTF8StringsDoNotRequireCredentials = fmt.Errorf("utf8 strings do not require credentials")

var _ interface {
	constructorruntime.ResourceInputMethod
	constructorruntime.SourceInputMethod
} = (*InputMethod)(nil)

// InputMethod processes UTF-8 input specifications into blobs.
type InputMethod struct{}

// NewInputMethod creates a new InputMethod.
func NewInputMethod() *InputMethod {
	return &InputMethod{}
}

func (i *InputMethod) GetResourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Resource) (runtime.Identity, error) {
	return nil, ErrUTF8StringsDoNotRequireCredentials
}

func (i *InputMethod) ProcessResource(_ context.Context, resource *constructorruntime.Resource, _ map[string]string) (*constructorruntime.ResourceInputMethodResult, error) {
	var utf8 v1.UTF8
	if err := Scheme.Convert(resource.Input, &utf8); err != nil {
		return nil, fmt.Errorf("error converting resource input spec: %w", err)
	}

	utf8Blob, err := GetV1UTF8Blob(utf8)
	if err != nil {
		return nil, fmt.Errorf("error getting utf8 blob based on resource input specification: %w", err)
	}

	return &constructorruntime.ResourceInputMethodResult{ProcessedBlobData: utf8Blob}, nil
}

func (i *InputMethod) GetSourceCredentialConsumerIdentity(_ context.Context, _ *constructorruntime.Source) (runtime.Identity, error) {
	return nil, ErrUTF8StringsDoNotRequireCredentials
}

func (i *InputMethod) ProcessSource(_ context.Context, src *constructorruntime.Source, _ map[string]string) (*constructorruntime.SourceInputMethodResult, error) {
	var utf8 v1.UTF8
	if err := Scheme.Convert(src.Input, &utf8); err != nil {
		return nil, fmt.Errorf("error converting source input spec: %w", err)
	}

	utf8Blob, err := GetV1UTF8Blob(utf8)
	if err != nil {
		return nil, fmt.Errorf("error getting utf8 blob based on source input specification: %w", err)
	}

	return &constructorruntime.SourceInputMethodResult{ProcessedBlobData: utf8Blob}, nil
}
