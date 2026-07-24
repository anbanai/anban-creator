package main

import "testing"

func TestManagedRuntimeProjectConcurrencyCap(t *testing.T) {
	for _, testCase := range []struct {
		executor string
		want     int
	}{
		{executor: "kubernetes", want: 1},
		{executor: "docker", want: 0},
	} {
		t.Run(testCase.executor, func(t *testing.T) {
			if got := managedRuntimeProjectConcurrencyCap(testCase.executor); got != testCase.want {
				t.Fatalf("managedRuntimeProjectConcurrencyCap(%q) = %d, want %d", testCase.executor, got, testCase.want)
			}
		})
	}
}
