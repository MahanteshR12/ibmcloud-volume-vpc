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

// Package provider ...
package provider

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"

	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	"github.com/IBM/ibmcloud-volume-interface/provider/auth"
	"github.com/stretchr/testify/assert"
)

func generateTestPrivateKey(t *testing.T) *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate test RSA key: %v", err)
	}
	return key
}

func TestTokenGenerator(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	tg := tokenGenerator{}
	assert.NotNil(t, tg)

	testPrivKey := generateTestPrivateKey(t)

	tests := []struct {
		name        string
		tokenKID    string
		creds       provider.ContextCredentials
		setup       func()
		err         bool
		wantNilSign bool
	}{
		{
			name:     "missing tokenKID should error",
			tokenKID: "",
			creds: provider.ContextCredentials{
				AuthType:     provider.IAMAccessToken,
				Credential:   TestProviderAccessToken,
				IAMAccountID: TestIKSAccountID,
			},
			setup:       func() { tg.privateKey = nil },
			err:         true,
			wantNilSign: true,
		},
		{
			name:     "invalid tokenKID 'sample_key' should throw error",
			tokenKID: "sample_key",
			creds: provider.ContextCredentials{
				AuthType:     provider.IAMAccessToken,
				Credential:   TestProviderAccessToken,
				IAMAccountID: TestIKSAccountID,
			},
			setup:       func() { tg.privateKey = nil },
			err:         true,
			wantNilSign: true,
		},
		{
			name:     "valid IAMAccessToken with UserID should pass",
			tokenKID: "no_sample_key",
			creds: provider.ContextCredentials{
				AuthType:     provider.IAMAccessToken,
				Credential:   TestProviderAccessToken,
				IAMAccountID: TestIKSAccountID,
				UserID:       TestIKSAccountID,
			},
			setup: func() {
				tg.privateKey = testPrivKey
			},
			err:         false,
			wantNilSign: false,
		},
		{
			name:     "IMSToken should pass",
			tokenKID: "no_sample_key",
			creds: provider.ContextCredentials{
				AuthType:     auth.IMSToken,
				Credential:   TestProviderAccessToken,
				IAMAccountID: TestIKSAccountID,
				UserID:       TestIKSAccountID,
			},
			setup: func() {
				tg.privateKey = testPrivKey
			},
			err:         false,
			wantNilSign: false,
		},
		{
			name:     "IMSToken with invalid tokenKID still passes",
			tokenKID: "sample_key_invalid",
			creds: provider.ContextCredentials{
				AuthType:     auth.IMSToken,
				Credential:   TestProviderAccessToken,
				IAMAccountID: TestIKSAccountID,
				UserID:       TestIKSAccountID,
			},
			setup: func() {
				tg.privateKey = testPrivKey
			},
			err:         false,
			wantNilSign: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tg.tokenKID = tt.tokenKID

			tt.setup()

			signedToken, err := tg.getServiceToken(tt.creds, *logger)

			if tt.err {
				assert.NotNil(t, err)
			} else {
				assert.Nil(t, err)
			}

			if tt.wantNilSign {
				assert.Nil(t, signedToken)
			} else {
				assert.NotNil(t, signedToken)
			}
		})
	}
}
