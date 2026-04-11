package internal

import (
	"fmt"
	"slices"

	ocispecv1 "github.com/opencontainers/image-spec/specs-go/v1"

	graphinternal "ocm.software/open-component-model/bindings/go/graph/internal"
	"ocm.software/open-component-model/bindings/go/oci/looseref"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	"ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/runtime"
)

func asUnstructured(typed runtime.Typed) (*runtime.Unstructured, error) {
	return graphinternal.AsUnstructured(typed)
}

func chooseAddType(repo runtime.Typed) (runtime.Type, error) {
	return graphinternal.ChooseAddType(repo)
}

func chooseGetLocalResourceType(repo runtime.Typed) (runtime.Type, error) {
	return graphinternal.ChooseGetLocalResourceType(repo)
}

func chooseAddLocalResourceType(repo runtime.Typed) (runtime.Type, error) {
	return graphinternal.ChooseAddLocalResourceType(repo)
}

func getReferenceName(imageReference string) (string, error) {
	if imageReference == "" {
		return "", fmt.Errorf("cannot get reference name from empty image reference")
	}
	imageRef, err := looseref.ParseReference(imageReference)
	if err != nil {
		return "", fmt.Errorf("invalid OCI image reference %q: %w", imageReference, err)
	}
	if imageRef.Repository == "" {
		return "", fmt.Errorf("invalid image reference %q: repository is required", imageReference)
	}
	referenceName := imageRef.Repository
	if imageRef.Tag != "" {
		referenceName += ":" + imageRef.Tag
	}
	return referenceName, nil
}

// AppendUniqueRepositories merges sources into targets, skipping duplicates.
func AppendUniqueRepositories(targets []runtime.Typed, sources []runtime.Typed) []runtime.Typed {
	for _, s := range sources {
		if !slices.ContainsFunc(targets, func(t runtime.Typed) bool {
			return RepositoryEqual(t, s)
		}) {
			targets = append(targets, s)
		}
	}
	return targets
}

// RepositoryEqual compares two runtime.Typed repository specs by their concrete fields.
func RepositoryEqual(a, b runtime.Typed) bool {
	switch at := a.(type) {
	case *oci.Repository:
		bt, ok := b.(*oci.Repository)
		return ok && at.BaseUrl == bt.BaseUrl && at.SubPath == bt.SubPath
	case *ctfv1.Repository:
		bt, ok := b.(*ctfv1.Repository)
		return ok && at.FilePath == bt.FilePath && at.AccessMode == bt.AccessMode
	default:
		return a == b
	}
}

// isOCICompliantManifest checks if a descriptor describes a manifest that is recognizable by OCI.
func isOCICompliantManifest(mediaType string) bool {
	switch mediaType {
	case ocispecv1.MediaTypeImageManifest,
		ocispecv1.MediaTypeImageIndex:
		return true
	default:
		return false
	}
}
