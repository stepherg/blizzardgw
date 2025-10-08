package webhook

import (
	"testing"
)

func TestExtractDeviceFromSource(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{"with service", "mac:06cbd937c9d2/BlizzardRDK", "mac:06cbd937c9d2"},
		{"without service", "mac:06cbd937c9d2", "mac:06cbd937c9d2"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractDeviceFromSource(tt.source)
			if result != tt.expected {
				t.Errorf("extractDeviceFromSource(%q) = %q, want %q", tt.source, result, tt.expected)
			}
		})
	}
}

func TestExtractServiceFromSource(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{"with service", "mac:06cbd937c9d2/BlizzardRDK", "BlizzardRDK"},
		{"without service", "mac:06cbd937c9d2", ""},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractServiceFromSource(tt.source)
			if result != tt.expected {
				t.Errorf("extractServiceFromSource(%q) = %q, want %q", tt.source, result, tt.expected)
			}
		})
	}
}

func TestExtractEventFromDestination(t *testing.T) {
	tests := []struct {
		name     string
		dest     string
		expected string
	}{
		{"full path", "event:Blizzard/Time/TimerElapsed", "Time.TimerElapsed"},
		{"simple", "event:Service/Event", "Event"},
		{"no prefix", "Blizzard/Time/TimerElapsed", "Time.TimerElapsed"},
		{"single part", "event:Event", "Event"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractEventFromDestination(tt.dest)
			if result != tt.expected {
				t.Errorf("extractEventFromDestination(%q) = %q, want %q", tt.dest, result, tt.expected)
			}
		})
	}
}
