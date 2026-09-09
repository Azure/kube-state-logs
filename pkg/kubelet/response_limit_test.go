// Copyright (c) Microsoft Corporation.
// Licensed under the MIT license.

package kubelet

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestClientResponseSizeLimit(t *testing.T) {
	for _, test := range []struct {
		name    string
		size    int
		wantErr bool
	}{
		{name: "at limit", size: maxResponseBytes},
		{name: "over limit", size: maxResponseBytes + 1, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := `{"items":[],"pods":[]}` + strings.Repeat(" ", test.size-len(`{"items":[],"pods":[]}`))
			client, _ := newTestClient(t, func(writer http.ResponseWriter, request *http.Request) {
				_, _ = io.WriteString(writer, body)
			}, "token")
			for _, endpoint := range []string{"/pods", "/stats/summary"} {
				var destination any
				err := client.get(context.Background(), endpoint, &destination)
				if (err != nil) != test.wantErr {
					t.Fatalf("%s error = %v, wantErr %v", endpoint, err, test.wantErr)
				}
				if test.wantErr && !strings.Contains(err.Error(), "response exceeded") {
					t.Fatalf("%s unexpected error: %v", endpoint, err)
				}
			}
		})
	}
}
