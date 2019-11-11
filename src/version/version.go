package version

var buildName string
var buildVersion string

// BuildName gets the current build name. This is usually injected if built
// from git, or returns "unknown" otherwise.
func BuildName() string {
	if buildName == "" {
		return "unknown"
	}
	return buildName
}

// BuildVersion gets the current build version. This is usually injected if
// built from git, or returns "unknown" otherwise.
func BuildVersion() string {
	if buildVersion == "" {
		return "unknown"
	}
	return buildVersion
}
