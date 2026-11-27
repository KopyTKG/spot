package config

// HasPassword reports whether the dialog asks for a password. Such a dialog is
// shown by the calling process itself, not by the daemon: what is typed then
// goes straight to the caller and leaves with it, instead of crossing a socket
// and staying in the memory of a process that lives for days.
func (c *Config) HasPassword() bool {
	for _, it := range c.Items {
		if it.Type == "password" {
			return true
		}
	}
	return false
}
