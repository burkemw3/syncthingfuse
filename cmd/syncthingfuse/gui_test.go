package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHumanSizeVerifications(t *testing.T) {
	// Arrange
	var api apiSvc
	mux := api.getMux()
	server := httptest.NewServer(mux)
	defer server.Close()

	// Act
	assertHumanSizeVerification(t, server, "512 MiB", true)
	assertHumanSizeVerification(t, server, "102 MiB", true)
	assertHumanSizeVerification(t, server, "102", true)

	assertHumanSizeVerification(t, server, "MiB", false)
	assertHumanSizeVerification(t, server, "foobar", false)
	assertHumanSizeVerification(t, server, "512m MB", false)
}

func assertHumanSizeVerification(t *testing.T, server *httptest.Server, input string, success bool) {
	// Act
	resp, err := http.Post(server.URL+"/api/verify/humansize", "application/x-www-form-urlencoded", strings.NewReader(input))

	// Assert
	if success {
		if err != nil {
			t.Error(input, err)
		}
		if resp.StatusCode != 200 {
			t.Errorf(input+"Received non-200 response: %d\n", resp.StatusCode)
		}
	} else {
		if resp.StatusCode != 500 {
			t.Errorf(input+" Received non-500 response: %d\n", resp.StatusCode)
		}
	}
}
