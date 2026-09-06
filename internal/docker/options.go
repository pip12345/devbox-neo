package docker

import (
	"encoding/csv"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"devbox/internal/config"
)

var volumeName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var optionName = regexp.MustCompile(`^--[a-z][a-z0-9-]*$`)

func ParseMount(value, workspace, userHome string) (Mount, error) {
	var mount Mount
	if strings.ContainsAny(value, "\x00\r\n") {
		return mount, fmt.Errorf("mount contains control characters")
	}
	delimiter := strings.LastIndex(value, ":/")
	if delimiter < 1 {
		return mount, fmt.Errorf("expected SOURCE:/absolute/target[:options]")
	}
	source := value[:delimiter]
	target, options, _ := strings.Cut(value[delimiter+1:], ":")
	if !path.IsAbs(target) || path.Clean(target) != target {
		return mount, fmt.Errorf("mount target must be absolute and canonical")
	}
	mount.Target = target
	if options != "" {
		seen := map[string]bool{}
		for _, option := range strings.Split(options, ",") {
			switch option {
			case "ro", "rw", "z", "Z", "consistent", "cached", "delegated", "private", "rprivate", "shared", "rshared", "slave", "rslave", "nocopy":
			default:
				return mount, fmt.Errorf("unsupported mount option")
			}
			if seen[option] {
				continue
			}
			seen[option] = true
			mount.Options = append(mount.Options, option)
		}
		if seen["ro"] && seen["rw"] {
			return mount, fmt.Errorf("mount cannot be both read-only and writable")
		}
		mount.ReadOnly = seen["ro"]
	}
	if source == "~" || strings.HasPrefix(source, "~/") {
		if userHome == "" {
			return mount, fmt.Errorf("host home is unavailable")
		}
		if source == "~" {
			source = userHome
		} else {
			source = filepath.Join(userHome, source[2:])
		}
	}
	if !filepath.IsAbs(source) && !strings.ContainsAny(source, "/\\") && !strings.HasPrefix(source, ".") {
		if !volumeName.MatchString(source) {
			return mount, fmt.Errorf("invalid volume name")
		}
		mount.Kind = "volume"
		mount.Source = source
		return mount, nil
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(workspace, source)
	}
	source, err := filepath.EvalSymlinks(source)
	if err != nil {
		return mount, fmt.Errorf("mount source is unavailable: %w", err)
	}
	info, err := os.Stat(source)
	if err != nil {
		return mount, err
	}
	if !info.IsDir() && !info.Mode().IsRegular() {
		return mount, fmt.Errorf("bind source must be a file or directory")
	}
	mount.Source = source
	mount.File = !info.IsDir()
	mount.Kind = "bind"
	if len(mount.Options) > 0 && strings.Contains(source, ":") {
		return mount, fmt.Errorf("mount options cannot be combined with a colon in the bind source")
	}
	return mount, nil
}
func ValidateStoredMount(m Mount) error {
	if !path.IsAbs(m.Target) || path.Clean(m.Target) != m.Target || strings.ContainsRune(m.Source+m.Target, '\x00') {
		return fmt.Errorf("invalid recorded mount path")
	}
	switch m.Kind {
	case "", "bind":
		if !filepath.IsAbs(m.Source) || filepath.Clean(m.Source) != m.Source {
			return fmt.Errorf("invalid recorded bind source")
		}
	case "volume":
		if !volumeName.MatchString(m.Source) {
			return fmt.Errorf("invalid recorded volume")
		}
	default:
		return fmt.Errorf("invalid mount kind")
	}
	return nil
}

func Overlap(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/") || a == "/" || b == "/"
}
func ValidateExtraTargets(mounts []Mount, protected []string) error {
	targets := []string{}
	for _, mount := range mounts {
		for _, reserved := range protected {
			if Overlap(mount.Target, reserved) {
				return fmt.Errorf("extra mount overlaps Devbox-owned target %s", reserved)
			}
		}
		for _, previous := range targets {
			if Overlap(mount.Target, previous) {
				return fmt.Errorf("extra mounts overlap")
			}
		}
		targets = append(targets, mount.Target)
	}
	return nil
}
func portRange(value string, allowEmpty bool) (int, error) {
	if value == "" && allowEmpty {
		return 1, nil
	}
	left, right, rangeSet := strings.Cut(value, "-")
	low, err := strconv.Atoi(left)
	if err != nil || low < 1 || low > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}
	if !rangeSet {
		return 1, nil
	}
	high, err := strconv.Atoi(right)
	if err != nil || high < low || high > 65535 {
		return 0, fmt.Errorf("invalid port range")
	}
	return high - low + 1, nil
}
func ValidatePort(value string) error {
	if strings.ContainsAny(value, " \t\r\n\x00") {
		return fmt.Errorf("invalid published port")
	}
	address := ""
	rest := value
	if strings.HasPrefix(rest, "[") {
		end := strings.Index(rest, "]:")
		if end < 0 {
			return fmt.Errorf("invalid bracketed host IP")
		}
		address = rest[1:end]
		rest = rest[end+2:]
	}
	parts := strings.Split(rest, ":")
	if len(parts) == 3 && address == "" {
		address = parts[0]
		parts = parts[1:]
	}
	if len(parts) < 1 || len(parts) > 2 {
		return fmt.Errorf("expected [HOST_IP:]HOST_PORT:CONTAINER_PORT or CONTAINER_PORT")
	}
	if address != "" && net.ParseIP(address) == nil {
		return fmt.Errorf("host address must be an IP")
	}
	container, protocol, hasProtocol := strings.Cut(parts[len(parts)-1], "/")
	if hasProtocol && protocol != "tcp" && protocol != "udp" && protocol != "sctp" {
		return fmt.Errorf("invalid port protocol")
	}
	width, err := portRange(container, false)
	if err != nil {
		return err
	}
	if len(parts) == 2 {
		hostWidth, err := portRange(parts[0], true)
		if err != nil {
			return err
		}
		if parts[0] != "" && hostWidth != width {
			return fmt.Errorf("host and container port ranges must have equal size")
		}
	}
	return nil
}

// ValidateRaw keeps the Docker CLI escape hatch while protecting boundaries the
// engine owns. Values must stay in their own token; no positional argv is allowed.
func ValidateRaw(args []string, protected []string, workspace, userHome string) error {
	boolean := map[string]bool{"--privileged": true, "--read-only": true, "--oom-kill-disable": true, "--tty": true, "--interactive": true, "--publish-all": true}
	for _, arg := range args {
		if strings.ContainsAny(arg, "\x00\r\n") {
			return fmt.Errorf("raw Docker option contains control characters")
		}
		flag, value, hasValue := strings.Cut(arg, "=")
		if !optionName.MatchString(flag) {
			return fmt.Errorf("raw Docker arguments must be --option=value or a supported boolean option")
		}
		if !hasValue && !boolean[flag] {
			return fmt.Errorf("value-taking raw Docker options require --option=value")
		}
		switch flag {
		case "--name", "--network", "--net", "--label-file", "--env-file", "--entrypoint", "--rm", "--user", "--workdir", "--init", "--volumes-from":
			return fmt.Errorf("raw Docker option %s is owned by Devbox", flag)
		case "--label":
			key, _, _ := strings.Cut(value, "=")
			if strings.HasPrefix(key, Namespace+".") || key == "devcontainer.metadata" {
				return fmt.Errorf("raw Docker option cannot override managed labels")
			}
		case "--env":
			if err := config.ValidateEnvAssignment(value); err != nil {
				return err
			}
		case "--volume":
			mount, err := ParseMount(value, workspace, userHome)
			if err != nil {
				return err
			}
			if mount.Kind == "bind" && !strings.HasPrefix(value, "/") {
				return fmt.Errorf("raw bind sources must be absolute; use --volume for workspace-relative paths")
			}
			if err = ValidateExtraTargets([]Mount{mount}, protected); err != nil {
				return err
			}
		case "--mount":
			fields, err := csv.NewReader(strings.NewReader(value)).Read()
			if err != nil {
				return fmt.Errorf("invalid --mount CSV")
			}
			target := ""
			kind := "volume"
			source := ""
			for _, field := range fields {
				key, val, _ := strings.Cut(field, "=")
				switch key {
				case "target", "dst", "destination":
					if target != "" {
						return fmt.Errorf("duplicate mount target")
					}
					target = val
				case "type":
					kind = val
				case "source", "src":
					source = val
				}
			}
			if !path.IsAbs(target) || path.Clean(target) != target {
				return fmt.Errorf("raw mount requires a canonical absolute target")
			}
			if err = ValidateExtraTargets([]Mount{{Target: target}}, protected); err != nil {
				return err
			}
			if kind == "volume" && source == "" {
				return fmt.Errorf("raw volumes require a named source for recorded recovery")
			}
			if kind == "bind" {
				if !filepath.IsAbs(source) {
					return fmt.Errorf("raw bind source must be absolute")
				}
				if _, err = os.Stat(source); err != nil {
					return fmt.Errorf("raw bind source is unavailable")
				}
			}
		case "--tmpfs":
			target, _, _ := strings.Cut(value, ":")
			if !path.IsAbs(target) || path.Clean(target) != target {
				return fmt.Errorf("tmpfs target must be canonical and absolute")
			}
			if err := ValidateExtraTargets([]Mount{{Target: target}}, protected); err != nil {
				return err
			}
		case "--add-host":
			name, _, _ := strings.Cut(value, ":")
			if strings.EqualFold(name, HostAlias) {
				return fmt.Errorf("host gateway alias is owned by Devbox")
			}
		case "--publish":
			if err := ValidatePort(value); err != nil {
				return err
			}
		}
	}
	return nil
}
