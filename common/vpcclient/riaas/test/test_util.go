/**
 * Copyright 2020 IBM Corp.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package test ...
package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/IBM/ibmcloud-volume-vpc/common/vpcclient/client"
	"github.com/IBM/ibmcloud-volume-vpc/common/vpcclient/models"
	"github.com/stretchr/testify/assert"
)

// SetupServer ...
func SetupServer(t *testing.T) (m *http.ServeMux, c client.SessionClient, teardown func()) {
	m = http.NewServeMux()
	s := httptest.NewServer(m)

	log := new(bytes.Buffer)

	queryValues := url.Values{"version": []string{models.APIVersion}}

	c = client.New(context.Background(), s.URL, queryValues, http.DefaultClient, "test-context", "default").WithDebug(log).WithAuthToken("auth-token")

	teardown = func() {
		s.Close()
		CheckTestFail(t, log)
	}

	return
}

// SetupMuxResponse ...
func SetupMuxResponse(t *testing.T, m *http.ServeMux, path string, expectedMethod string, expectedContent *string, statusCode int, body string, verify func(t *testing.T, r *http.Request)) {
	m.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, expectedMethod, r.Method)

		authHeader := r.Header.Get("Authorization")
		assert.Equal(t, "Bearer auth-token", authHeader)

		acceptHeader := r.Header.Get("Accept")
		assert.Equal(t, "application/json", acceptHeader)

		if expectedContent != nil {
			b, _ := io.ReadAll(r.Body)
			assert.Equal(t, *expectedContent, string(b))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)

		if body != "" {
			fmt.Fprint(w, body)
		}

		if verify != nil {
			verify(t, r)
		}
	})
}

// CheckTestFail ...
func CheckTestFail(t *testing.T, buf *bytes.Buffer) {
	if t.Failed() {
		t.Log(buf)
	}
}
