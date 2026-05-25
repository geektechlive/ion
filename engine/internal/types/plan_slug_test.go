package types

import "testing"

// TestPlanSlugFromPath covers the canonical helper that derives the
// human-readable plan slug from a plan file path. Mirrors the
// session-package wrapper test (plan_slug_test.go in internal/session)
// but exercises the public helper directly, so consumers that import
// types and call PlanSlugFromPath (the desktop main process via JSON
// round-trip, or future Go SDK extensions) are covered.
func TestPlanSlugFromPath(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"unix word slug", "/home/user/.ion/plans/happy-jumping-rabbit.md", "happy-jumping-rabbit"},
		{"unix repo plans", "/repo/.ion/plans/calm-baking-otter.md", "calm-baking-otter"},
		{"unix legacy hex", "/legacy/plans/ef072eb2660d0993109be0862df6328d.md", "ef072eb2660d0993109be0862df6328d"},
		{"no md extension", "/plans/just-a-name", "just-a-name"},
		{"relative unix path", "./plans/cool-running-stream.md", "cool-running-stream"},
		{"windows-style separators", `C:\Users\u\.ion\plans\bright-soaring-eagle.md`, "bright-soaring-eagle"},
		{"only separator unix", "/", ""},
		{"only separator windows", `\`, ""},
		{"dot", ".", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := PlanSlugFromPath(tc.in); got != tc.want {
				t.Errorf("PlanSlugFromPath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
