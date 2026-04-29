package componentversion

import (
	"encoding/json"
	"fmt"

	graphPkg "ocm.software/open-component-model/bindings/go/transform/graph"
	graphRuntime "ocm.software/open-component-model/bindings/go/transform/graph/runtime"
	"ocm.software/open-component-model/cli/internal/render/progress"
	"ocm.software/open-component-model/cli/internal/render/progress/bar"
)

func mapEvent(e graphRuntime.ProgressEvent) progress.Event[*graphPkg.Transformation] {
	return progress.Event[*graphPkg.Transformation]{
		ID:    e.Transformation.ID,
		Name:  fmt.Sprintf("%s [%s]", e.Transformation.ID, e.Transformation.Type.Name),
		State: mapState(e.State),
		Err:   e.Err,
		Data:  e.Transformation,
	}
}

func mapState(s graphRuntime.State) progress.State {
	switch s {
	case graphRuntime.Running:
		return progress.Running
	case graphRuntime.Completed:
		return progress.Completed
	case graphRuntime.Failed:
		return progress.Failed
	default:
		return progress.Unknown
	}
}

func formatError(t *graphPkg.Transformation, err error) string {
	result := bar.SidebarText("", bar.TreeErrorFormatter(err), bar.Red)

	if t != nil && t.Spec != nil {
		info := fmt.Sprintf("Transformation %q of type %s/%s failed.\nSpec data shown below for debugging.",
			t.ID, t.Type.Name, t.Type.Version)
		specJSON, jsonErr := json.MarshalIndent(t.Spec.Data, "", "  ")
		if jsonErr == nil {
			result += bar.FramedText(info, string(specJSON), 4)
		}
	}

	return result
}
