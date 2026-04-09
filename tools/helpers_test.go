package tools

import (
	"encoding/json"
	"testing"
	"time"
)

func TestAgeString(t *testing.T) {
	cases := []struct {
		dur      time.Duration
		expected string
	}{
		{30 * time.Second, "30s"},
		{5 * time.Minute, "5m"},
		{3 * time.Hour, "3h"},
		{48 * time.Hour, "2d"},
	}
	for _, c := range cases {
		t.Run(c.expected, func(t *testing.T) {
			got := ageString(time.Now().Add(-c.dur))
			if got != c.expected {
				t.Errorf("expected %q, got %q", c.expected, got)
			}
		})
	}
}

func TestToJSON(t *testing.T) {
	type Sample struct {
		Name string `json:"name"`
	}
	result := toJSON(Sample{Name: "test"})
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(result), &m); err != nil {
		t.Fatalf("toJSON produced invalid JSON: %v", err)
	}
	if m["name"] != "test" {
		t.Errorf("unexpected value: %v", m)
	}
}
