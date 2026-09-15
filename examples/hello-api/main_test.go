package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestParsePort(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "default", value: "", want: 8080},
		{name: "valid", value: "9000", want: 9000},
		{name: "zero", value: "0", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "too large", value: "65536", wantErr: true},
		{name: "not a number", value: "http", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePort(tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parsePort(%q) error = %v, wantErr %v", tt.value, err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("parsePort(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestHealth(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	recorder := httptest.NewRecorder()

	routes().ServeHTTP(recorder, request)

	response := recorder.Result()
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want %d", response.StatusCode, http.StatusOK)
	}
	if got := recorder.Body.String(); got != "ok\n" {
		t.Errorf("GET /healthz body = %q, want %q", got, "ok\\n")
	}
}

func TestRoot(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	recorder := httptest.NewRecorder()

	routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET / status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if got := recorder.Body.String(); got != "Hello API is running\n" {
		t.Errorf("GET / body = %q, want %q", got, "Hello API is running\\n")
	}
}
