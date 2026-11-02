package agent

import "testing"

func TestNormalizeDockerfileImageRefs(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			name: "registry mirror is stripped",
			body: "FROM registry.example.com/team/node:24.22.0 AS builder\nRUN echo hi",
			want: "FROM node:24.22.0 AS builder\nRUN echo hi",
		},
		{
			name: "registry with port and two-level namespace",
			body: "FROM localhost:5000/team/runtime/app:1.2.3",
			want: "FROM app:1.2.3",
		},
		{
			name: "official library image is unchanged",
			body: "FROM nginx:alpine",
			want: "FROM nginx:alpine",
		},
		{
			name: "namespace without registry is unchanged",
			body: "FROM oven/bun:latest AS build",
			want: "FROM oven/bun:latest AS build",
		},
		{
			name: "digest is preserved",
			body: "FROM registry.example.com/team/app@sha256:abc123",
			want: "FROM app@sha256:abc123",
		},
		{
			name: "platform flag with equals is preserved",
			body: "FROM --platform=linux/amd64 registry.example.com/team/node:24.22.0 AS builder",
			want: "FROM --platform=linux/amd64 node:24.22.0 AS builder",
		},
		{
			name: "platform flag with separate value is preserved",
			body: "FROM --platform linux/amd64 registry.example.com/team/node:24.22.0",
			want: "FROM --platform linux/amd64 node:24.22.0",
		},
		{
			name: "build variable is unchanged",
			body: "FROM ${STUDIO_PREVIOUS_IMAGE} AS previous",
			want: "FROM ${STUDIO_PREVIOUS_IMAGE} AS previous",
		},
		{
			name: "lowercase from is normalized",
			body: "from registry.example.com/team/alpine:3.20",
			want: "from alpine:3.20",
		},
		{
			name: "non-FROM registry mentions are untouched",
			body: "RUN echo registry.example.com/team/node:1",
			want: "RUN echo registry.example.com/team/node:1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeDockerfileImageRefs(tc.body); got != tc.want {
				t.Fatalf("normalizeDockerfileImageRefs(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}
