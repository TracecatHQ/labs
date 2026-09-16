package main

import "testing"

func TestCuratedTargetsHaveFiveUniqueFixturesPerClass(t *testing.T) {
	targets := []string{
		"n8n",
		"bash/CVE-2014-6271",
		"httpd/CVE-2021-41773",
		"python/CVE-2024-23334",
		"httpd/CVE-2021-40438",
		"langflow/CVE-2025-3248",
		"metabase/CVE-2023-38646",
		"cmsms/CVE-2019-9053",
	}
	for _, target := range targets {
		malicious, benign, err := fixtures(target)
		if err != nil {
			t.Fatalf("%s: %v", target, err)
		}
		if len(malicious) != 5 || len(benign) != 5 {
			t.Fatalf("%s: got %d malicious and %d benign fixtures", target, len(malicious), len(benign))
		}
		seen := map[string]bool{}
		for _, fixture := range append(malicious, benign...) {
			if fixture.ID == "" || fixture.Method == "" || fixture.Path == "" {
				t.Fatalf("%s: incomplete fixture", target)
			}
			if seen[fixture.ID] {
				t.Fatalf("%s: duplicate fixture ID %s", target, fixture.ID)
			}
			seen[fixture.ID] = true
		}
	}
}
