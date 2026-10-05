package migration

import (
	"slices"

	"devbox/internal/environment"
	"devbox/internal/harness"
	"devbox/internal/store"
)

// Verify the immediately preceding record before compacting it. These shapes
// exist only in the explicit cutover; ordinary fingerprinting uses tree digests.
func legacyFingerprints(r store.Record, contexts []map[string]environment.FileInput, files map[string]environment.FileInput) environment.Fingerprints {
	states := func(files map[string]environment.FileInput) map[string]environment.FileState {
		if files == nil {
			return nil
		}
		result := map[string]environment.FileState{}
		for name, f := range files {
			result[name] = f.FileState
		}
		return result
	}
	hooks := func(files []environment.FileInput) []environment.FileState {
		var result []environment.FileState
		for _, f := range files {
			result = append(result, f.FileState)
		}
		return result
	}
	type stage struct {
		Dockerfile, Ignore environment.FileState
		Context            map[string]environment.FileState
	}
	var stages []stage
	for i, s := range r.Applied.Inputs.Image.Stages {
		stages = append(stages, stage{s.Dockerfile.FileState, s.Ignore.FileState, states(contexts[i])})
	}
	i := r.Applied.Inputs.Image
	image := environment.Digest(struct {
		Base, Harness, Definition, Prepared, Boundary, Layer string
		Stages                                               []stage
		Arguments                                            map[string]string
	}{i.BaseImage, i.Harness, i.Definition.Hash, i.Prepared, i.Boundary, i.Layer, stages, i.Arguments})
	container := r.Applied.Inputs.Container
	container.Setup = slices.Clone(container.Setup)
	for i := range container.Setup {
		container.Setup[i].Source = ""
	}
	creation := environment.Digest(struct {
		Image  string
		Inputs environment.ContainerInputs
	}{image, container})
	creation = environment.Digest(struct{ Inputs, ImageID string }{creation, r.Applied.ImageID})
	runtime := r.Applied.Inputs.Runtime
	launch := environment.Digest(struct {
		Assets      string
		Files       map[string]environment.FileState
		BeforeOpen  []environment.FileState
		Launch      harness.Launch
		Args, Shell []string
	}{runtime.Assets, states(files), hooks(runtime.BeforeOpen), runtime.Launch, runtime.Args, runtime.Shell})
	return environment.Fingerprints{Image: image, Container: creation, Runtime: launch}
}
