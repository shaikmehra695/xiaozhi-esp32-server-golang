package controllers

import "testing"

func TestNormalizeDeviceNameCandidate(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "colon mac", in: "28:0A:C6:1D:3B:E8", want: "280ac61d3be8"},
		{name: "dash mac", in: "28-0A-C6-1D-3B-E8", want: "280ac61d3be8"},
		{name: "dot mac", in: "280A.C61D.3BE8", want: "280ac61d3be8"},
		{name: "full width colon", in: "28：0A：C6：1D：3B：E8", want: "280ac61d3be8"},
		{name: "plain mac", in: "280AC61D3BE8", want: "280ac61d3be8"},
		{name: "with spaces", in: " 28:0A:C6:1D:3B:E8 ", want: "280ac61d3be8"},
		{name: "empty", in: "   ", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeDeviceNameCandidate(tt.in); got != tt.want {
				t.Fatalf("normalizeDeviceNameCandidate(%q)=%q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
