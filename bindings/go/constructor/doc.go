// Package constructor provides functionality for creating and managing Open Component Model (OCM)
// component versions through a transformation graph. It converts a component constructor
// specification into a graph of transformations that are executed to build and publish
// component versions to a target repository.
//
// # Architecture
//
// The package uses a two-phase approach:
//
//  1. Graph Definition: [BuildGraphDefinition] converts a [constructor.ComponentConstructor]
//     specification into a [transformv1alpha1.TransformationGraphDefinition] that describes
//     all operations needed to construct the component versions.
//
//  2. Graph Execution: [NewDefaultBuilder] creates a pre-configured builder with all
//     transformers needed to execute the graph (OCI/CTF uploads, file/utf8/dir/helm
//     input processing, and component digest computation).
//
// # Usage
//
//	tgd, err := constructor.BuildGraphDefinition(ctx, spec,
//	    constructor.WithTargetRepository(repoSpec),
//	    constructor.WithWorkingDirectory(workDir),
//	)
//
//	b := constructor.NewDefaultBuilder(repoProvider, credResolver)
//	graph, err := b.BuildAndCheck(tgd)
//	err = graph.Process(ctx)
//
// # Options
//
//   - [WithTargetRepository]: MANDATORY. Sets the target repository specification.
//   - [WithWorkingDirectory]: Sets the working directory for resolving relative paths.
//   - [WithExternalComponentRepository]: Resolves external component references.
//   - [WithConflictPolicy]: Controls behavior when a component version already exists.
//   - [WithExternalCopyPolicy]: Controls whether external references are copied.
//   - [WithSkipDigestProcessing]: Skips component digest computation.
package constructor
