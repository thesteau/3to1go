// Package buildinfo holds the release version and commit stamped into the
// binary at build time, for example:
//
//	go build -ldflags "-X github.com/3to1go/shared/buildinfo.Version=v1.2.0 -X github.com/3to1go/shared/buildinfo.Commit=<sha>"
//
// The Dockerfiles set both from the VERSION and REVISION build arguments.
// Stable images built from prod set both. The latest images built from main
// set only the commit.
package buildinfo

// Version is the release tag, such as v1.2.0, or empty for builds that are not
// releases.
var Version = ""

// Commit is the Git commit the binary was built from, or empty when unknown.
var Commit = ""

// ShortCommit returns the first seven characters of Commit.
func ShortCommit() string {
	if len(Commit) > 7 {
		return Commit[:7]
	}
	return Commit
}

// Summary names the build for people: the release tag for stable builds, the
// short commit for main builds, and "dev" for local builds.
func Summary() string {
	switch {
	case Version != "":
		return Version
	case Commit != "":
		return ShortCommit()
	default:
		return "dev"
	}
}

// Fields returns the build details for JSON responses.
func Fields() map[string]string {
	return map[string]string{"version": Version, "commit": Commit, "summary": Summary()}
}
