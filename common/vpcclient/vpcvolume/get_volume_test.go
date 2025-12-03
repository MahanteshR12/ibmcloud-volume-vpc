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

package vpcvolume_test

import (
	"net/http"
	"os"
	"testing"

	"github.com/IBM/ibmcloud-volume-vpc/common/vpcclient/models"
	"github.com/IBM/ibmcloud-volume-vpc/common/vpcclient/riaas/test"
	"github.com/IBM/ibmcloud-volume-vpc/common/vpcclient/vpcvolume"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func GetTestContextLogger() (*zap.Logger, zap.AtomicLevel) {
	consoleDebugging := zapcore.Lock(os.Stdout)
	consoleErrors := zapcore.Lock(os.Stderr)
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "ts"
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	traceLevel := zap.NewAtomicLevel()
	traceLevel.SetLevel(zap.InfoLevel)
	core := zapcore.NewTee(
		zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), consoleDebugging, zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return (lvl >= traceLevel.Level()) && (lvl < zapcore.ErrorLevel)
		})),
		zapcore.NewCore(zapcore.NewJSONEncoder(encoderConfig), consoleErrors, zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
			return lvl >= zapcore.ErrorLevel
		})),
	)
	logger := zap.New(core, zap.AddCaller())
	return logger, traceLevel
}

func TestGetVolume(t *testing.T) {
	// Setup new style zap logger
	logger, _ := GetTestContextLogger()
	defer logger.Sync()

	testCases := []struct {
		name string

		// Response
		status  int
		content string

		// Expected return
		expectErr string
		verify    func(*testing.T, *models.Volume, error)
	}{
		{
			name:   "Verify that the correct endpoint is invoked",
			status: http.StatusNoContent,
		}, {
			name:      "Verify that a 404 is returned to the caller",
			status:    http.StatusNotFound,
			content:   "{\"errors\":[{\"message\":\"testerr\",\"Code\":\"volume_not_found\"}], \"trace\":\"2af63776-4df7-4970-b52d-4e25676ec0e4\"}",
			expectErr: "Trace Code:2af63776-4df7-4970-b52d-4e25676ec0e4, Code:volume_not_found, Description:testerr, RC:404 Not Found",
		}, {
			name:    "Verify that the volume is parsed correctly",
			status:  http.StatusOK,
			content: "{\"id\":\"vol1\",\"name\":\"vol1\",\"capacity\":10,\"iops\":3000,\"status\":\"pending\",\"zone\":{\"name\":\"test-1\",\"href\":\"https://us-south.iaas.cloud.ibm.com/v1/regions/us-south/zones/test-1\"},\"crn\":\"crn:v1:bluemix:public:is:test-1:a/rg1::volume:vol1\"}",
			verify: func(t *testing.T, volume *models.Volume, err error) {
				if assert.NotNil(t, volume) {
					assert.Equal(t, "vol1", volume.ID)
				}
			},
		}, {
			name:    "False positive: What if the volume ID is not matched",
			status:  http.StatusOK,
			content: "{\"id\":\"wrong-vol\",\"name\":\"wrong-vol\",\"capacity\":10,\"iops\":3000,\"status\":\"pending\",\"zone\":{\"name\":\"test-1\",\"href\":\"https://us-south.iaas.cloud.ibm.com/v1/regions/us-south/zones/test-1\"},\"crn\":\"crn:v1:bluemix:public:is:test-1:a/rg1::volume:wrong-vol\"}",
			verify: func(t *testing.T, volume *models.Volume, err error) {
				if assert.NotNil(t, volume) {
					assert.NotEqual(t, "vol1", volume.ID)
				}
			},
		},
	}

	for _, testcase := range testCases {
		t.Run(testcase.name, func(t *testing.T) {
			mux, client, teardown := test.SetupServer(t)
			emptyString := ""
			test.SetupMuxResponse(t, mux, vpcvolume.Version+"/volumes/volume-id", http.MethodGet, &emptyString, testcase.status, testcase.content, nil)

			defer teardown()

			logger.Info("Test case being executed", zap.Reflect("testcase", testcase.name))

			volumeService := vpcvolume.New(client)

			volume, err := volumeService.GetVolume("volume-id", logger)
			logger.Info("Volume details", zap.Reflect("volume", volume))

			if testcase.expectErr != "" && assert.Error(t, err) {
				assert.Equal(t, testcase.expectErr, err.Error())
				assert.Nil(t, volume)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, volume)
			}

			if testcase.verify != nil {
				testcase.verify(t, volume, err)
			}
		})
	}
}

func TestGetVolumeByName(t *testing.T) {
	// Setup new style zap logger
	logger, _ := GetTestContextLogger()
	defer logger.Sync()

	testCases := []struct {
		name string

		// Response
		status  int
		volumes string
		content string

		// Expected return
		expectErr string
		verify    func(*testing.T, *models.Volume, error)
	}{
		{
			name:   "Verify that the correct endpoint is invoked",
			status: http.StatusNoContent,
		}, {
			name:      "Verify that a 404 is returned to the caller",
			status:    http.StatusNotFound,
			content:   "{\"errors\":[{\"message\":\"testerr\",\"Code\":\"volume_not_found\"}], \"trace\":\"2af63776-4df7-4970-b52d-4e25676ec0e4\"}",
			expectErr: "Trace Code:2af63776-4df7-4970-b52d-4e25676ec0e4, Code:volume_not_found, Description:testerr, RC:404 Not Found",
		}, {
			name:    "Verify that the volume name is parsed correctly",
			status:  http.StatusOK,
			content: "{\"volumes\":[{\"id\":\"vol1\",\"name\":\"vol1\",\"capacity\":10,\"iops\":3000,\"status\":\"pending\",\"zone\":{\"name\":\"test-1\",\"href\":\"https://us-south.iaas.cloud.ibm.com/v1/regions/us-south/zones/test-1\"},\"crn\":\"crn:v1:bluemix:public:is:test-1:a/rg1::volume:vol1\"}]}",
			verify: func(t *testing.T, volume *models.Volume, err error) {
				if assert.NotNil(t, volume) {
					assert.Equal(t, "vol1", volume.ID)
				}
			},
		}, {
			name:    "Verify that the volume is empty if the volumes are empty",
			status:  http.StatusOK,
			content: "{\"volumes\":[{\"id\":\"vol2\",\"name\":\"vol2\",\"size\":10,\"iops\":3000,\"status\":\"pending\",\"zone\":{\"name\":\"test-1\",\"href\":\"https://us-south.iaas.cloud.ibm.com/v1/regions/us-south/zones/test-1\"},\"crn\":\"crn:v1:bluemix:public:is:test-1:a/rg1::volume:vol2\"}]}",
		},
	}

	for _, testcase := range testCases {
		t.Run(testcase.name, func(t *testing.T) {
			mux, client, teardown := test.SetupServer(t)
			emptyString := ""
			test.SetupMuxResponse(t, mux, vpcvolume.Version+"/volumes", http.MethodGet, &emptyString, testcase.status, testcase.content, nil)

			defer teardown()

			logger.Info("Test case being executed", zap.Reflect("testcase", testcase.name))

			volumeService := vpcvolume.New(client)
			volume, err := volumeService.GetVolumeByName("vol1", logger)
			logger.Info("Volume details", zap.Reflect("volume", volume))

			if testcase.verify != nil {
				testcase.verify(t, volume, err)
			}
		})
	}
}
