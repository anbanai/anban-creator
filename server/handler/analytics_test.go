package handler

import "testing"

func TestDecodeAnalyticsContentID(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "encoded target key", raw: "seednote_post%3A02a3bd48-3025-4e73-a786-3c13ed033638", want: "seednote_post:02a3bd48-3025-4e73-a786-3c13ed033638"},
		{name: "already decoded target key", raw: "seednote_post:02a3bd48-3025-4e73-a786-3c13ed033638", want: "seednote_post:02a3bd48-3025-4e73-a786-3c13ed033638"},
		{name: "malformed escape", raw: "seednote_post%3", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := decodeAnalyticsContentID(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("decoded ID = %q, want %q", got, tt.want)
			}
		})
	}
}
