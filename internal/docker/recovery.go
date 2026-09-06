package docker

import (
	"context"
	"encoding/csv"
	"path/filepath"
	"strings"
)

// Named user volumes are external inputs. Recovery must not let Docker create
// an empty replacement merely because the old volume disappeared.
func (r Runtime) CheckRawVolumes(ctx context.Context, args []string) error {
	for _, arg := range args {
		flag, value, _ := strings.Cut(arg, "=")
		volume := ""
		switch flag {
		case "--volume":
			delimiter := strings.LastIndex(value, ":/")
			if delimiter > 0 && !filepath.IsAbs(value[:delimiter]) {
				volume = value[:delimiter]
			}
		case "--mount":
			fields, err := csv.NewReader(strings.NewReader(value)).Read()
			if err != nil {
				return err
			}
			kind := "volume"
			source := ""
			for _, field := range fields {
				key, v, _ := strings.Cut(field, "=")
				switch key {
				case "type":
					kind = v
				case "source", "src":
					source = v
				}
			}
			if kind == "volume" {
				volume = source
			}
		}
		if volume != "" {
			if err := r.Volume(ctx, volume); err != nil {
				return err
			}
		}
	}
	return nil
}
