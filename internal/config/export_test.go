package config

// LoadFrom exposes loadFrom for tests so config discovery can be exercised
// against a temp directory without mutating the process working directory.
func LoadFrom(start string) (*Config, error) {
	return loadFrom(start)
}

// InitAt exposes initAt for tests, for the same reason as LoadFrom.
func InitAt(start string) (string, error) {
	return initAt(start)
}
