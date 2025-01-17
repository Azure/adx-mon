package main

import "testing"

func TestConfigValidity(t *testing.T) {
	contents := getContents()

	for _, section := range contents.Sections {
		if err := section.Config.Validate(); err != nil {
			t.Errorf("invalid config for section %s: %v", section.Title, err)
		}
	}
}
