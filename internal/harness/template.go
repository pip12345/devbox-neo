package harness

import "strings"

// Definitions have one Devbox template variable. Shell expansion, where chosen
// explicitly by install/prepare commands, remains the container shell's job.
func (d *Definition) expandUser() {
	expand := func(s string) string { return strings.ReplaceAll(s, "${user}", "devuser") }
	argv := func(values []string) {
		for i, v := range values {
			values[i] = expand(v)
		}
	}
	d.Binary = expand(d.Binary)
	d.Install.Shell = expand(d.Install.Shell)
	argv(d.Install.Path)
	argv(d.Launch.Args)
	argv(d.Launch.Continue)
	for k, v := range d.Env {
		d.Env[k] = expand(v)
	}
	for i := range d.Stores {
		d.Stores[i].Target = expand(d.Stores[i].Target)
	}
	d.Config.Path = expand(d.Config.Path)
	for i := range d.Auth {
		d.Auth[i].Source = expand(d.Auth[i].Source)
		d.Auth[i].Target = expand(d.Auth[i].Target)
	}
	for _, a := range d.Prepare {
		argv(a)
	}
}
