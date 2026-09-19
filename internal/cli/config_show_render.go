package cli

import (
	"io"
	"sort"

	"devbox/internal/resource"
)

func printConfigView(out io.Writer, view resource.ConfigView) error {
	width := configDisplayWidth(out)
	if err := writeConfigLine(out, view.Scope+" configuration: ", displayCell(view.Path), "  ", width); err != nil {
		return err
	}
	for _, layer := range view.Trace.Layers {
		text := displayCell(layer.Name)
		if layer.Path != "" {
			text += " (" + displayCell(layer.Path) + ")"
		}
		if err := writeConfigLine(out, "  layer: ", text, "    ", width); err != nil {
			return err
		}
	}
	for _, excluded := range view.Trace.Excluded {
		if err := writeConfigLine(out, "  excluded: ", displayCell(excluded), "    ", width); err != nil {
			return err
		}
	}
	rows := []configDisplayRow{}
	var add func(string, any)
	add = func(key string, value any) {
		if object, ok := value.(map[string]any); ok {
			if len(object) > 0 {
				keys := make([]string, 0, len(object))
				for child := range object {
					keys = append(keys, child)
				}
				sort.Strings(keys)
				for _, child := range keys {
					// Dotted paths match the resolver's nested-field provenance keys.
					add(key+"."+child, object[child])
				}
				return
			}
			value = nil
		}
		rows = append(rows, configDisplayRow{
			label: displayCell(key), value: value,
			origin:       configSourceForScope(view.Scope, configSourceLabel(view.Trace.Sources[key])),
			entryOrigins: configEntryOrigins(view.Scope, view.Trace.EntrySources[key]),
			command:      key == "shell",
		})
	}
	keys := make([]string, 0, len(view.Values))
	for key := range view.Values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		add(key, view.Values[key])
	}
	if err := printConfigRows(out, rows, "  ", width); err != nil {
		return err
	}
	keys = nil
	for key := range view.Trace.Winners {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := writeConfigLine(out, displayCell(key)+": ", displayCell(view.Trace.Winners[key]), "  ", width); err != nil {
			return err
		}
	}
	if view.Harness != nil {
		if err := writeConfigLine(out, "Harness: ", displayCell(view.Harness["name"])+" ("+displayCell(view.Harness["origin"])+")", "  ", width); err != nil {
			return err
		}
	}
	return nil
}
