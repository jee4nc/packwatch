package security

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// fakeOSV serves the OSV batch and vuln-detail endpoints. Installed version
// 1.0.0 has GHSA-a and GHSA-b; version 2.0.0 still has GHSA-b. When
// failAvailable is set, batch queries for 2.0.0 return HTTP 500.
func fakeOSV(t *testing.T, failAvailable bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/querybatch", func(w http.ResponseWriter, r *http.Request) {
		var req osvBatchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode batch request: %v", err)
		}
		var resp osvBatchResponse
		for _, q := range req.Queries {
			switch q.Version {
			case "1.0.0":
				resp.Results = append(resp.Results, osvBatchResult{Vulns: []osvVulnRef{{ID: "GHSA-a"}, {ID: "GHSA-b"}}})
			case "2.0.0":
				if failAvailable {
					http.Error(w, "boom", http.StatusInternalServerError)
					return
				}
				resp.Results = append(resp.Results, osvBatchResult{Vulns: []osvVulnRef{{ID: "GHSA-b"}}})
			default:
				resp.Results = append(resp.Results, osvBatchResult{})
			}
		}
		json.NewEncoder(w).Encode(resp)
	})
	mux.HandleFunc("/v1/vulns/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/v1/vulns/")
		json.NewEncoder(w).Encode(osvVulnDetail{
			ID:               id,
			DatabaseSpecific: map[string]interface{}{"severity": "HIGH"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	oldBatch, oldVuln := osvBatchURL, osvVulnURL
	osvBatchURL, osvVulnURL = srv.URL+"/v1/querybatch", srv.URL+"/v1/vulns"
	t.Cleanup(func() { osvBatchURL, osvVulnURL = oldBatch, oldVuln })
	return srv
}

func TestCheckFixedByUpdate(t *testing.T) {
	fakeOSV(t, false)

	results := Check([]Query{{Name: "pkg", InstalledVersion: "1.0.0", AvailableVersion: "2.0.0"}}, nil)

	r := results[0]
	if len(r.Vulnerabilities) != 2 {
		t.Fatalf("got %d vulnerabilities, want 2", len(r.Vulnerabilities))
	}
	if r.FixedByUpdate != 1 {
		t.Errorf("FixedByUpdate = %d, want 1", r.FixedByUpdate)
	}
	for _, v := range r.Vulnerabilities {
		if want := v.ID == "GHSA-a"; v.Fixed != want {
			t.Errorf("%s Fixed = %v, want %v", v.ID, v.Fixed, want)
		}
	}
}

func TestCheckAvailableQueryFailureIsNotFixed(t *testing.T) {
	fakeOSV(t, true)

	results := Check([]Query{{Name: "pkg", InstalledVersion: "1.0.0", AvailableVersion: "2.0.0"}}, nil)

	r := results[0]
	if len(r.Vulnerabilities) != 2 {
		t.Fatalf("got %d vulnerabilities, want 2", len(r.Vulnerabilities))
	}
	if r.FixedByUpdate != 0 {
		t.Errorf("FixedByUpdate = %d, want 0 when the available-version query fails", r.FixedByUpdate)
	}
	for _, v := range r.Vulnerabilities {
		if v.Fixed {
			t.Errorf("%s marked as fixed although the available-version query failed", v.ID)
		}
	}
}
