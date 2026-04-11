package construct

import (
	"context"
	"fmt"

	graph "ocm.software/open-component-model/bindings/go/graph/construct/internal"
	constructor "ocm.software/open-component-model/bindings/go/constructor/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
)

// BuildGraphDefinition constructs a [transformv1alpha1.TransformationGraphDefinition] that
// describes how to construct component versions from a [constructor.ComponentConstructor].
//
// The target repository must be specified via [WithTargetRepository].
func BuildGraphDefinition(
	ctx context.Context,
	spec *constructor.ComponentConstructor,
	opts ...Option,
) (*transformv1alpha1.TransformationGraphDefinition, error) {
	o := ConstructOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}

	if o.TargetRepositorySpec == nil {
		return nil, fmt.Errorf("target repository spec is required: use WithTargetRepository")
	}

	return graph.BuildGraphDefinition(ctx, spec,
		o.TargetRepositorySpec,
		o.WorkingDirectory,
		o.ExternalComponentRepositoryResolver,
		graph.ExternalComponentVersionCopyPolicy(o.ExternalComponentVersionCopyPolicy),
		graph.ComponentVersionConflictPolicy(o.ComponentVersionConflictPolicy),
		o.SkipDigestProcessing,
	)
}
