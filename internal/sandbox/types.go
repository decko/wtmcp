package sandbox

// PluginInfo holds the plugin metadata needed to build a sandbox
// profile. Avoids importing internal/plugin (circular dependency).
type PluginInfo struct {
	Name            string
	Dir             string
	Handler         string
	CredentialGroup string
	SessionDir      string // User's project directory (read access)
	OutputDir       string // Per-plugin output directory (write access)
}
