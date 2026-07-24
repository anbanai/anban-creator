package main

import "testing"

func TestManagedRuntimeProjectConcurrencyCap(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		executor string
		want     int
	}{
		{name: "Docker managed runtime", executor: "docker", want: 1},
		{name: "Kubernetes managed runtime", executor: "kubernetes", want: 1},
		{name: "unknown executor", executor: "unknown", want: 0},
		{name: "empty executor", executor: "", want: 0},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := managedRuntimeProjectConcurrencyCap(testCase.executor); got != testCase.want {
				t.Fatalf("managedRuntimeProjectConcurrencyCap(%q) = %d, want %d", testCase.executor, got, testCase.want)
			}
		})
	}
}
