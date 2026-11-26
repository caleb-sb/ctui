package main

import (
	"reflect"
	"testing"
)

func TestExampleConfigMatchesDefaults(t *testing.T) {
	t.Setenv("CTUI_CONFIG", "config.example.toml")
	got, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want := defaultConfig()
	want.Root = expandHome(want.Root)
	got.ClaudeArgs = nil
	if !reflect.DeepEqual(got, want) {
		t.Errorf("example config differs from defaults:\n got %+v\nwant %+v", got, want)
	}
}

func TestNormalizeTicket(t *testing.T) {
	c := defaultConfig()
	for in, want := range map[string]string{"5170": "PROJ-5170", "proj-5170": "PROJ-5170", " PROJ-1 ": "PROJ-1"} {
		if got, err := c.NormalizeTicket(in); err != nil || got != want {
			t.Errorf("NormalizeTicket(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	if _, err := c.NormalizeTicket("PROJ 5170; rm -rf"); err == nil {
		t.Error("expected error for unsafe ticket")
	}
}
