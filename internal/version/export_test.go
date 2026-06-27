package version

// NormalizeSemver exposes normalizeSemver for white-box table tests.
func NormalizeSemver(v string) string {
	return normalizeSemver(v)
}
