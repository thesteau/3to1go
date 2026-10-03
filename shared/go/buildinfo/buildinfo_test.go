package buildinfo

import "testing"

func TestSummary(t *testing.T) {
	oldVersion, oldCommit := Version, Commit
	t.Cleanup(func() { Version, Commit = oldVersion, oldCommit })

	const sha = "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b"
	cases := []struct{ name, version, commit, want string }{
		{"prod build","v1.2.0", sha, "v1.2.0"},
		{"main build", "", sha, "1a2b3c4"},
		{"local build", "", "", "dev"},
	}
	for _, tc := range cases {
		Version, Commit = tc.version, tc.commit
		if got := Summary(); got != tc.want {
			t.Errorf("%s: Summary() = %q, want %q", tc.name, got, tc.want)
		}
		if got := Fields(); got["summary"] != tc.want || got["commit"] != tc.commit {
			t.Errorf("%s: Fields() = %v", tc.name, got)
		}
	}
}
