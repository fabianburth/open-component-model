package componentversion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"ocm.software/open-component-model/bindings/go/constructor"
	constructorruntime "ocm.software/open-component-model/bindings/go/constructor/runtime"
	constructorv1 "ocm.software/open-component-model/bindings/go/constructor/v1"
	"ocm.software/open-component-model/bindings/go/oci/compref"
	ocires "ocm.software/open-component-model/bindings/go/oci/repository/resource"
	ctfv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/ctf"
	ociv1 "ocm.software/open-component-model/bindings/go/oci/spec/repository/v1/oci"
	"ocm.software/open-component-model/bindings/go/repository"
	"ocm.software/open-component-model/bindings/go/repository/component/resolvers"
	"ocm.software/open-component-model/bindings/go/runtime"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	transformv1alpha1 "ocm.software/open-component-model/bindings/go/transform/spec/v1alpha1"
	"ocm.software/open-component-model/cli/cmd/setup"
	ocmctx "ocm.software/open-component-model/cli/internal/context"
	"ocm.software/open-component-model/cli/internal/flags/enum"
	"ocm.software/open-component-model/cli/internal/flags/file"
	"ocm.software/open-component-model/cli/internal/flags/log"
	"ocm.software/open-component-model/cli/internal/render"
	"ocm.software/open-component-model/cli/internal/repository/ocm"
)

const (
	FlagRepositoryRef                      = "repository"
	FlagComponentConstructorPath           = "constructor"
	FlagComponentVersionConflictPolicy     = "component-version-conflict-policy"
	FlagExternalComponentVersionCopyPolicy = "external-component-version-copy-policy"
	FlagSkipReferenceDigestProcessing      = "skip-reference-digest-processing"
	FlagDryRun                             = "dry-run"
	FlagOutput                             = "output"

	DefaultComponentConstructorBaseName = "component-constructor"
	LegacyDefaultArchiveName            = "transport-archive"

	// Each node emits 2 events (Running + Completed/Failed) and since the renderer consumes
	// them faster than the constructor produces, 16 is enough to avoid blocking with room to grow.
	eventBufferSize = 16
)

type ComponentVersionConflictPolicy string

const (
	ComponentVersionConflictPolicyAbortAndFail ComponentVersionConflictPolicy = "abort-and-fail"
	ComponentVersionConflictPolicySkip         ComponentVersionConflictPolicy = "skip"
	ComponentVersionConflictPolicyReplace      ComponentVersionConflictPolicy = "replace"
)

type ExternalComponentVersionCopyPolicy string

const (
	ExternalComponentVersionCopyPolicyCopyOrFail ExternalComponentVersionCopyPolicy = "copy-or-fail"
	ExternalComponentVersionCopyPolicySkip       ExternalComponentVersionCopyPolicy = "skip"
)

func (p ExternalComponentVersionCopyPolicy) ToConstructorPolicy() constructor.ExternalComponentVersionCopyPolicy {
	switch p {
	case ExternalComponentVersionCopyPolicyCopyOrFail:
		return constructor.ExternalComponentVersionCopyPolicyCopyOrFail
	case ExternalComponentVersionCopyPolicySkip:
		return constructor.ExternalComponentVersionCopyPolicySkip
	default:
		return constructor.ExternalComponentVersionCopyPolicySkip
	}
}

func ExternalComponentVersionCopyPolicies() []string {
	return []string{
		string(ExternalComponentVersionCopyPolicySkip),
		string(ExternalComponentVersionCopyPolicyCopyOrFail),
	}
}

func (p ComponentVersionConflictPolicy) ToConstructorConflictPolicy() constructor.ComponentVersionConflictPolicy {
	switch p {
	case ComponentVersionConflictPolicyReplace:
		return constructor.ComponentVersionConflictReplace
	case ComponentVersionConflictPolicySkip:
		return constructor.ComponentVersionConflictSkip
	default:
		return constructor.ComponentVersionConflictAbortAndFail
	}
}

func ComponentVersionConflictPolicies() []string {
	return []string{
		string(ComponentVersionConflictPolicyAbortAndFail),
		string(ComponentVersionConflictPolicySkip),
		string(ComponentVersionConflictPolicyReplace),
	}
}

func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:        "component-version",
		Aliases:    []string{"cv", "componentversion", "component-versions", "cvs", "componentversions"},
		SuggestFor: []string{"component", "components", "version", "versions"},
		Short:      fmt.Sprintf("Add component version(s) to an OCM Repository based on a %[1]q file", DefaultComponentConstructorBaseName),
		Args:       cobra.NoArgs,
		Long: fmt.Sprintf(`Add component version(s) to an OCM repository that can be reused for transfers.

A %[1]q file is used to specify the component version(s) to be added. It can contain both a single component or many components.

By default, the command will look for a file named "%[1]s.yaml" or "%[1]s.yml" in the current directory.
If given a path to a directory, the command will look for a file named "%[1]s.yaml" or "%[1]s.yml" in that directory.
If given a path to a file, the command will attempt to use that file as the %[1]q file.

If you provide a working directory, all paths in the %[1]q file will be resolved relative to that directory.
Otherwise the path to the %[1]q file will be used as the working directory.
You are only allowed to reference files within the working directory or sub-directories of the working directory.

Environment Variable Substitution:

The %[1]q file supports environment variable substitution using Go template syntax.
Variables can be referenced using ${VAR_NAME} or $VAR_NAME format.
All environment variables are expanded before the file is processed, allowing for dynamic
configuration of component versions, resource paths, image references, and other values.

Example:
  components:
    - name: ${COMPONENT_NAME}
      version: ${COMPONENT_VERSION}
      provider:
        name: ${PROVIDER_NAME}
      resources:
        - name: my-image
          type: ociImage
          version: ${COMPONENT_VERSION}
          access:
            type: ociArtifact
            imageReference: ${REGISTRY_URL}/my-app:${IMAGE_TAG}

Repository Reference Format:
	[type::]{repository}

For known types, currently only {%[2]s} are supported, which can be shortened to {%[3]s} respectively for convenience.

If no type is given, the repository specification is interpreted based on introspection and heuristics:

- URL schemes or domain patterns -> OCI registry
- Local paths -> CTF archive

In case the CTF archive does not exist, it will be created by default.
If not specified, it will be created with the name "transport-archive".
`,
			DefaultComponentConstructorBaseName,
			strings.Join([]string{ociv1.Type, ctfv1.Type}, "|"),
			strings.Join([]string{ociv1.ShortType, ociv1.ShortType2, ctfv1.ShortType, ctfv1.ShortType2}, "|"),
		),
		Example: strings.TrimSpace(fmt.Sprintf(`
Adding component versions to a CTF archive:

add component-version --%[1]s ./path/to/transport-archive --%[2]s ./path/to/%[3]s.yaml
add component-version --%[1]s /tmp/my-archive --%[2]s constructor.yaml

Adding component versions to an OCI registry:

add component-version --%[1]s ghcr.io/my-org/my-repo --%[2]s %[3]s.yaml
add component-version --%[1]s https://my-registry.com/my-repo --%[2]s %[3]s.yaml
add component-version --%[1]s localhost:5000/my-repo --%[2]s %[3]s.yaml

Specifying repository types explicitly:

add component-version --%[1]s ctf::./local/archive --%[2]s %[3]s.yaml
add component-version --%[1]s oci::http://localhost:8080/my-repo --%[2]s %[3]s.yaml

Using environment variables in %[3]q files:

export COMPONENT_NAME="github.com/my-org/my-app"
export COMPONENT_VERSION="1.2.3"
export REGISTRY_URL="ghcr.io/my-org"
add component-version --%[1]s ./archive --%[2]s %[3]s.yaml
`, FlagRepositoryRef, FlagComponentConstructorPath, DefaultComponentConstructorBaseName)),
		RunE:              AddComponentVersion,
		PersistentPreRunE: persistentPreRunE,
		DisableAutoGenTag: true,
	}

	cmd.Flags().StringP(FlagRepositoryRef, string(FlagRepositoryRef[0]), LegacyDefaultArchiveName, "repository ref")
	file.VarP(cmd.Flags(), FlagComponentConstructorPath, string(FlagComponentConstructorPath[0]), DefaultComponentConstructorBaseName+".yaml", "path to the component constructor file")
	enum.Var(cmd.Flags(), FlagComponentVersionConflictPolicy, ComponentVersionConflictPolicies(), "policy to apply when a component version already exists in the repository")
	enum.Var(cmd.Flags(), FlagExternalComponentVersionCopyPolicy, ExternalComponentVersionCopyPolicies(), "policy to apply when a component reference to a component version outside of the constructor or target repository is encountered")
	cmd.Flags().Bool(FlagSkipReferenceDigestProcessing, false, "skip digest processing for resources and sources. Any resource referenced via access type will not have their digest updated.")
	cmd.Flags().Bool(FlagDryRun, false, "build and validate the graph but do not execute")
	enum.VarP(cmd.Flags(), FlagOutput, "o", []string{render.OutputFormatYAML.String(), render.OutputFormatJSON.String(), render.OutputFormatNDJSON.String()}, "output format of the transformation graph definition (dry-run only)")

	return cmd
}

func persistentPreRunE(cmd *cobra.Command, _ []string) error {
	logger, err := log.GetBaseLogger(cmd)
	if err != nil {
		return fmt.Errorf("get logger: %w", err)
	}
	slog.SetDefault(logger)

	// Load OCM config and filesystem config first so that WorkingDirectory
	// (from the config file or CLI flag) is available before we resolve the
	// constructor file path.
	if err := setup.OCMConfig(cmd); err != nil {
		return fmt.Errorf("setup ocm config: %w", err)
	}
	setup.FilesystemConfig(cmd, setup.FilesystemConfigOptions{})

	constructorFile, err := getComponentConstructorFile(cmd)
	if err != nil {
		return fmt.Errorf("getting component constructor failed: %w", err)
	}

	// If the working directory still isn't set (neither CLI flag nor config
	// file provided one), fall back to the constructor file's parent directory
	// and re-apply the filesystem config so downstream setup (e.g. plugin
	// manager) sees it.
	ctx := cmd.Context()
	if fsCfg := ocmctx.FromContext(ctx).FilesystemConfig(); fsCfg == nil || fsCfg.WorkingDirectory == "" {
		path := constructorFile.String()
		if path, err = filepath.Abs(path); err != nil {
			return err
		}
		wd := filepath.Dir(path)
		slog.DebugContext(ctx, "setting working directory from constructorFile path",
			slog.String("working-directory", wd))
		setup.FilesystemConfig(cmd, setup.FilesystemConfigOptions{WorkingDirectory: wd})
	}

	// Remaining setup that depends on filesystem config being complete.
	if err := setup.PluginManager(cmd); err != nil {
		return fmt.Errorf("setup plugin manager: %w", err)
	}
	if err := setup.CredentialGraph(cmd); err != nil {
		return fmt.Errorf("setup credential graph: %w", err)
	}
	ocmctx.Register(cmd)

	if parent := cmd.Parent(); parent != nil {
		cmd.SetOut(parent.OutOrStdout())
		cmd.SetErr(parent.ErrOrStderr())
	}

	return nil
}

func AddComponentVersion(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	octx := ocmctx.FromContext(ctx)

	pm := octx.PluginManager()
	if pm == nil {
		return fmt.Errorf("could not retrieve plugin manager from context")
	}

	credGraph := octx.CredentialGraph()
	if credGraph == nil {
		return fmt.Errorf("could not retrieve credential graph from context")
	}

	skipReferenceDigestProcessing, err := cmd.Flags().GetBool(FlagSkipReferenceDigestProcessing)
	if err != nil {
		return fmt.Errorf("getting skip-reference-digest-processing flag failed: %w", err)
	}

	dryRun, err := cmd.Flags().GetBool(FlagDryRun)
	if err != nil {
		return fmt.Errorf("getting dry-run flag failed: %w", err)
	}

	output, err := enum.Get(cmd.Flags(), FlagOutput)
	if err != nil {
		return fmt.Errorf("getting output flag failed: %w", err)
	}

	cvConflictPolicy, err := enum.Get(cmd.Flags(), FlagComponentVersionConflictPolicy)
	if err != nil {
		return fmt.Errorf("getting component-version-conflict-policy flag failed: %w", err)
	}

	evCopyPolicy, err := enum.Get(cmd.Flags(), FlagExternalComponentVersionCopyPolicy)
	if err != nil {
		return fmt.Errorf("getting external-component-version-copy-policy flag failed: %w", err)
	}

	repoSpec, err := GetRepositorySpec(cmd)
	if err != nil {
		return fmt.Errorf("getting repository spec failed: %w", err)
	}

	constructorFile, err := getComponentConstructorFile(cmd)
	if err != nil {
		return fmt.Errorf("getting component constructor path failed: %w", err)
	}

	constructorSpec, err := GetComponentConstructor(constructorFile)
	if err != nil {
		return fmt.Errorf("getting component constructor failed: %w", err)
	}

	repositoryRef, err := cmd.Flags().GetString(FlagRepositoryRef)
	if err != nil {
		return fmt.Errorf("getting repository reference flag failed: %w", err)
	}

	config := octx.Configuration()
	ref, err := compref.ParseRepository(repositoryRef, compref.WithCTFAccessMode(ctfv1.AccessModeCreate+"|"+ctfv1.AccessModeReadWrite))
	if err != nil {
		return fmt.Errorf("parsing repository reference %q failed: %w", repositoryRef, err)
	}

	repoResolver, err := ocm.NewComponentRepositoryResolver(ctx,
		pm.ComponentVersionRepositoryRegistry,
		credGraph,
		ocm.WithRepository(ref), ocm.WithConfig(config),
	)
	if err != nil {
		return fmt.Errorf("could not initialize ocm repository: %w", err)
	}

	// Determine working directory from filesystem config
	var workingDir string
	if fsCfg := octx.FilesystemConfig(); fsCfg != nil {
		workingDir = fsCfg.WorkingDirectory
	}

	// Pre-flight conflict check: verify component versions don't already exist
	// when the policy requires it. This must happen before building the graph
	// because the graph builder doesn't have repository access.
	conflictPolicy := ComponentVersionConflictPolicy(cvConflictPolicy).ToConstructorConflictPolicy()
	if conflictPolicy != constructor.ComponentVersionConflictReplace {
		var kept []constructorruntime.Component
		for i := range constructorSpec.Components {
			comp := &constructorSpec.Components[i]
			repo, err := repoResolver.GetComponentVersionRepositoryForComponent(ctx, comp.Name, comp.Version)
			if err != nil {
				slog.DebugContext(ctx, "could not resolve repository for conflict check, skipping check",
					"component", comp.Name, "version", comp.Version, "error", err)
				kept = append(kept, *comp)
				continue
			}
			if repo == nil {
				kept = append(kept, *comp)
				continue
			}
			_, err = repo.GetComponentVersion(ctx, comp.Name, comp.Version)
			if err == nil {
				// Component version exists
				if conflictPolicy == constructor.ComponentVersionConflictAbortAndFail {
					return fmt.Errorf("component version %q already exists in target repository",
						comp.ToIdentity())
				}
				// Skip policy
				slog.WarnContext(ctx, "component version already exists, skipping",
					"component", comp.Name, "version", comp.Version)
				continue
			}
			kept = append(kept, *comp)
		}
		constructorSpec.Components = kept
	}

	// Build transformation graph definition
	tgd, err := constructor.BuildGraphDefinition(ctx, constructorSpec,
		constructor.WithTargetRepository(repoSpec),
		constructor.WithWorkingDirectory(workingDir),
		constructor.WithExternalComponentRepository(&externalRepoProvider{resolver: repoResolver}),
		constructor.WithConflictPolicy(ComponentVersionConflictPolicy(cvConflictPolicy).ToConstructorConflictPolicy()),
		constructor.WithExternalCopyPolicy(ExternalComponentVersionCopyPolicy(evCopyPolicy).ToConstructorPolicy()),
		constructor.WithSkipDigestProcessing(skipReferenceDigestProcessing),
	)
	if err != nil {
		return fmt.Errorf("building graph definition failed: %w", err)
	}

	// Build transformer builder
	var digestProcessor repository.ResourceDigestProcessor
	if !skipReferenceDigestProcessing {
		digestProcessor = ocires.NewResourceRepository(octx.FilesystemConfig())
	}
	b := constructor.NewDefaultBuilder(pm.ComponentVersionRepositoryRegistry, credGraph, digestProcessor)
	graph, err := b.
		WithEvents(make(chan graphRuntime.ProgressEvent, eventBufferSize)).
		BuildAndCheck(tgd)
	if err != nil {
		reader, rerr := renderTGD(tgd, output)
		if rerr != nil {
			return fmt.Errorf("graph build failed: %w (rendering also failed: %w)", err, rerr)
		}
		defer func() {
			if reader != nil {
				_ = reader.Close()
			}
		}()
		raw, readErr := io.ReadAll(reader)
		if readErr != nil {
			return fmt.Errorf("graph build failed: %w (reading render also failed: %w)", err, readErr)
		}
		if len(raw) == 0 {
			return fmt.Errorf("graph build failed: %w", err)
		}
		return fmt.Errorf("graph build failed: %w\n%s", err, raw)
	}

	if dryRun {
		reader, err := renderTGD(tgd, output)
		if err != nil {
			return fmt.Errorf("rendering transformation graph failed: %w", err)
		}
		defer func() {
			if err := reader.Close(); err != nil {
				slog.WarnContext(ctx, "closing transformation graph reader failed", "error", err)
			}
		}()
		if _, err := io.Copy(cmd.OutOrStdout(), reader); err != nil {
			return fmt.Errorf("writing transformation graph failed: %w", err)
		}
		return nil
	}

	// Create event channel and tracker
	tracker := newProgressTracker(graph, cmd.OutOrStdout())
	go tracker.Start(ctx)

	// Execute graph
	if err := graph.Process(ctx); err != nil {
		tracker.Summary(err)
		return fmt.Errorf("construction failed: %w", err)
	}
	tracker.Summary(nil)

	slog.DebugContext(ctx, "construction completed successfully")
	return nil
}

func GetRepositorySpec(cmd *cobra.Command) (runtime.Typed, error) {
	repositoryRef, err := cmd.Flags().GetString(FlagRepositoryRef)
	if err != nil {
		return nil, fmt.Errorf("getting repository reference flag failed: %w", err)
	}

	typed, err := compref.ParseRepository(repositoryRef)
	if err != nil {
		return nil, fmt.Errorf("failed to parse repository: %w", err)
	}

	if ctfRepo, ok := typed.(*ctfv1.Repository); ok {
		logger, err := log.GetBaseLogger(cmd)
		if err != nil {
			return nil, fmt.Errorf("getting base logger failed: %w", err)
		}

		var accessMode ctfv1.AccessMode = ctfv1.AccessModeReadWrite
		if _, err := os.Stat(ctfRepo.FilePath); os.IsNotExist(err) {
			accessMode += "|" + ctfv1.AccessModeCreate
		}

		logger.Debug("setting access mode for CTF repository", "path", ctfRepo.FilePath, "ref", repositoryRef, "mode", accessMode)
		ctfRepo.AccessMode = accessMode
	}

	return typed, nil
}

func GetComponentConstructor(file *file.Flag) (*constructorruntime.ComponentConstructor, error) {
	path := file.String()
	constructorStream, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("opening component constructor %q failed: %w", path, err)
	}
	defer func() { _ = constructorStream.Close() }()
	constructorData, err := io.ReadAll(constructorStream)
	if err != nil {
		return nil, fmt.Errorf("reading component constructor %q failed: %w", path, err)
	}
	// Perform environment variable substitution on the constructor file content.
	constructorData = []byte(os.Expand(string(constructorData), os.Getenv))

	data := constructorv1.ComponentConstructor{}
	if err := yaml.Unmarshal(constructorData, &data); err != nil {
		return nil, fmt.Errorf("unmarshalling component constructor %q failed: %w", path, err)
	}

	return constructorruntime.ConvertToRuntimeConstructor(&data), nil
}

func getComponentConstructorFile(cmd *cobra.Command) (*file.Flag, error) {
	constructorFlag, err := file.Get(cmd.Flags(), FlagComponentConstructorPath)
	if err != nil {
		return nil, fmt.Errorf("getting component constructor path flag failed: %w", err)
	}

	// When a working directory is configured (via CLI flag or ocm config) and
	// the constructor path is relative, resolve it against that directory
	// instead of the process cwd.
	if !filepath.IsAbs(constructorFlag.String()) {
		if fsCfg := ocmctx.FromContext(cmd.Context()).FilesystemConfig(); fsCfg != nil && fsCfg.WorkingDirectory != "" {
			resolved := filepath.Join(fsCfg.WorkingDirectory, constructorFlag.String())
			if err := constructorFlag.Set(resolved); err != nil {
				return nil, fmt.Errorf("resolving constructor path against working directory failed: %w", err)
			}
		}
	}

	if !constructorFlag.Exists() {
		return nil, fmt.Errorf("component constructor %q does not exist", constructorFlag.String())
	} else if constructorFlag.IsDir() {
		return nil, fmt.Errorf("path %q is a directory but must point to a component constructor", constructorFlag.String())
	}
	return constructorFlag, nil
}

// externalRepoProvider wraps a ComponentVersionRepositoryResolver to implement
// the constructor.ExternalComponentRepositoryProvider interface.
type externalRepoProvider struct {
	resolver resolvers.ComponentVersionRepositoryResolver
}

func (p *externalRepoProvider) GetExternalRepository(ctx context.Context, name, version string) (repository.ComponentVersionRepository, error) {
	if p.resolver == nil {
		return nil, fmt.Errorf("cannot fetch external component version %s:%s: no repository provider configured", name, version)
	}
	return p.resolver.GetComponentVersionRepositoryForComponent(ctx, name, version)
}

func renderTGD(tgd *transformv1alpha1.TransformationGraphDefinition, format string) (io.ReadCloser, error) {
	switch format {
	case render.OutputFormatJSON.String():
		read, write := io.Pipe()
		encoder := json.NewEncoder(write)
		encoder.SetIndent("", "  ")
		go func() {
			err := encoder.Encode(tgd)
			_ = write.CloseWithError(err)
		}()
		return read, nil
	case render.OutputFormatNDJSON.String():
		read, write := io.Pipe()
		encoder := json.NewEncoder(write)
		go func() {
			err := encoder.Encode(tgd)
			_ = write.CloseWithError(err)
		}()
		return read, nil
	case render.OutputFormatYAML.String():
		data, err := yaml.Marshal(tgd)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(data)), nil
	default:
		return nil, fmt.Errorf("invalid output format %q", format)
	}
}
