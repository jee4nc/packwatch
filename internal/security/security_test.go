package security

import (
	"testing"
)

func TestNormalizeSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"CRITICAL", "CRITICAL"},
		{"critical", "CRITICAL"},
		{"HIGH", "HIGH"},
		{"high", "HIGH"},
		{"MODERATE", "MEDIUM"},
		{"moderate", "MEDIUM"},
		{"MEDIUM", "MEDIUM"},
		{"medium", "MEDIUM"},
		{"LOW", "LOW"},
		{"low", "LOW"},
		{"something", "UNKNOWN"},
		{"", "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := normalizeSeverity(tt.input)
			if got != tt.want {
				t.Errorf("normalizeSeverity(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestCvssToLevel(t *testing.T) {
	tests := []struct {
		name  string
		score string
		want  string
	}{
		{"all high", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", "CRITICAL"},
		{"two high", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:L", "HIGH"},
		{"one high", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:L/A:N", "MEDIUM"},
		{"no high", "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:L/I:L/A:N", "LOW"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cvssToLevel(tt.score)
			if got != tt.want {
				t.Errorf("cvssToLevel(%q) = %q, want %q", tt.score, got, tt.want)
			}
		})
	}
}

func TestExtractSeverity(t *testing.T) {
	tests := []struct {
		name   string
		detail osvVulnDetail
		want   string
	}{
		{
			name: "database_specific severity",
			detail: osvVulnDetail{
				DatabaseSpecific: map[string]interface{}{"severity": "HIGH"},
			},
			want: "HIGH",
		},
		{
			name: "GHSA moderate",
			detail: osvVulnDetail{
				DatabaseSpecific: map[string]interface{}{"severity": "MODERATE"},
			},
			want: "MEDIUM",
		},
		{
			name: "fallback to CVSS",
			detail: osvVulnDetail{
				Severity: []osvSeverity{
					{Type: "CVSS_V3", Score: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"},
				},
			},
			want: "CRITICAL",
		},
		{
			name:   "no severity info",
			detail: osvVulnDetail{},
			want:   "UNKNOWN",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSeverity(tt.detail)
			if got != tt.want {
				t.Errorf("extractSeverity() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractAdvisoryURL(t *testing.T) {
	tests := []struct {
		name   string
		detail osvVulnDetail
		want   string
	}{
		{
			name: "ADVISORY type preferred",
			detail: osvVulnDetail{
				References: []osvReference{
					{Type: "WEB", URL: "https://example.com"},
					{Type: "ADVISORY", URL: "https://github.com/advisories/GHSA-1234"},
				},
			},
			want: "https://github.com/advisories/GHSA-1234",
		},
		{
			name: "fallback to WEB",
			detail: osvVulnDetail{
				References: []osvReference{
					{Type: "WEB", URL: "https://example.com"},
				},
			},
			want: "https://example.com",
		},
		{
			name:   "no references",
			detail: osvVulnDetail{},
			want:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAdvisoryURL(tt.detail)
			if got != tt.want {
				t.Errorf("extractAdvisoryURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSeverityRanking(t *testing.T) {
	if severityRank["CRITICAL"] <= severityRank["HIGH"] {
		t.Error("CRITICAL should rank higher than HIGH")
	}
	if severityRank["HIGH"] <= severityRank["MEDIUM"] {
		t.Error("HIGH should rank higher than MEDIUM")
	}
	if severityRank["MEDIUM"] <= severityRank["LOW"] {
		t.Error("MEDIUM should rank higher than LOW")
	}
	if severityRank["LOW"] <= severityRank[""] {
		t.Error("LOW should rank higher than empty")
	}
}

func TestSortVulns(t *testing.T) {
	vulns := []Vulnerability{
		{ID: "low-1", Severity: "LOW"},
		{ID: "critical-1", Severity: "CRITICAL"},
		{ID: "high-1", Severity: "HIGH"},
		{ID: "medium-1", Severity: "MEDIUM"},
	}

	sortVulns(vulns)

	expected := []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"}
	for i, v := range vulns {
		if v.Severity != expected[i] {
			t.Errorf("sortVulns: index %d severity = %q, want %q", i, v.Severity, expected[i])
		}
	}
}

func TestVulnerabilityFixedField(t *testing.T) {
	v := Vulnerability{
		ID:       "GHSA-1234",
		Summary:  "Test vulnerability",
		Severity: "HIGH",
		Fixed:    true,
	}

	if !v.Fixed {
		t.Error("expected Fixed to be true")
	}
	if v.ID != "GHSA-1234" {
		t.Errorf("expected ID GHSA-1234, got %s", v.ID)
	}
}

func TestPackageResultFixedByUpdate(t *testing.T) {
	result := PackageResult{
		Name:    "test-pkg",
		Version: "1.0.0",
		Vulnerabilities: []Vulnerability{
			{ID: "v1", Severity: "HIGH", Fixed: true},
			{ID: "v2", Severity: "LOW", Fixed: true},
			{ID: "v3", Severity: "MEDIUM", Fixed: false},
		},
		HighestSeverity: "HIGH",
		FixedByUpdate:   2,
	}

	if result.FixedByUpdate != 2 {
		t.Errorf("expected FixedByUpdate=2, got %d", result.FixedByUpdate)
	}

	fixedCount := 0
	for _, v := range result.Vulnerabilities {
		if v.Fixed {
			fixedCount++
		}
	}
	if fixedCount != 2 {
		t.Errorf("expected 2 vulns with Fixed=true, got %d", fixedCount)
	}
}

func TestQueryFields(t *testing.T) {
	q := Query{
		Name:             "express",
		InstalledVersion: "4.17.1",
		AvailableVersion: "4.19.2",
	}

	if q.Name != "express" {
		t.Errorf("expected Name=express, got %s", q.Name)
	}
	if q.InstalledVersion != "4.17.1" {
		t.Errorf("expected InstalledVersion=4.17.1, got %s", q.InstalledVersion)
	}
	if q.AvailableVersion != "4.19.2" {
		t.Errorf("expected AvailableVersion=4.19.2, got %s", q.AvailableVersion)
	}
}
