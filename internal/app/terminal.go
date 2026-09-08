package app

// TerminalEnv captures display capabilities for this invocation. They describe
// the attaching terminal, not desired configuration or durable session identity.
func TerminalEnv(lookup func(string) (string, bool)) []string {
	keys := []string{
		"TERM", "COLORTERM", "NO_COLOR", "CLICOLOR", "CLICOLOR_FORCE", "FORCE_COLOR",
		"TERM_PROGRAM", "TERM_PROGRAM_VERSION", "WT_SESSION", "WEZTERM_EXECUTABLE",
		"KITTY_WINDOW_ID", "VTE_VERSION", "KONSOLE_VERSION", "ITERM_SESSION_ID",
	}
	var env []string
	for _, key := range keys {
		if value, ok := lookup(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return env
}
