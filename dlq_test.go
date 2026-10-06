package main

import "testing"

func TestFailedBuildIsDiagnosed(t *testing.T) {
	cases := []struct {
		name   string
		status string
		want   bool
	}{{"failure", "failed", true}, {"success", "passed", false}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := failed(BuildEvent{Status: tc.status})
			if got != tc.want {
				t.Fatalf("failed(%q)=%v, want %v", tc.status, got, tc.want)
			}
		})
	}
}

func TestDiagnosticNamesCommit(t *testing.T) {
	d := diagnose(BuildEvent{Job: "release", Commit: "deadbeef", Status: "failed"})
	if d.Commit != "deadbeef" || d.Job != "release" || d.Reason == "" {
		t.Fatalf("unexpected diagnostic: %+v", d)
	}
}
