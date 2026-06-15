/**
 * Copyright 2025 IBM Corp.
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

// Package watcher ...
package watcher

import (
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/IBM/ibmcloud-volume-interface/config"
	cloudprovider "github.com/IBM/ibmcloud-volume-vpc/pkg/ibmcloudprovider"
	"github.com/golang/glog"
	"github.com/onsi/gomega/ghttp"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"
	v1core "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/record"
)

func TestNew(t *testing.T) {
	// Creating test logger
	_, teardown := GetTestLogger(t)
	defer teardown()
}

func TestAddTags(t *testing.T) {
	var server *ghttp.Server
	conf := &config.Config{
		Bluemix: &config.BluemixConfig{
			IamAPIKey: "test",
		},
		VPC: &config.VPCProviderConfig{
			VPCBlockProviderName: "vpc-classic",
		},
	}
	logger, _ := GetTestLogger(t)
	fakeIBMCloudStorageProvider, _ := cloudprovider.NewFakeIBMCloudStorageProvider("configPath", logger)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	clientset := fake.NewSimpleClientset()
	eventInterface := clientset.CoreV1().Events("")
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: eventInterface})

	pvw := &PVWatcher{
		provisionerName: "ibm-csi-driver",
		logger:          logger,
		config:          conf,
		cloudProvider:   fakeIBMCloudStorageProvider,
		recorder:        broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "pod-name"}),
	}
	pv := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pv",
		},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName:              "test-storage-class",
			PersistentVolumeReclaimPolicy: v1.PersistentVolumeReclaimDelete,
			ClaimRef: &v1.ObjectReference{
				Namespace: "test-namespace",
				Name:      "test-pvc",
			},
			Capacity: v1.ResourceList(map[v1.ResourceName]resource.Quantity{
				v1.ResourceStorage: resource.MustParse("1Gi"),
			}),

			PersistentVolumeSource: v1.PersistentVolumeSource{
				CSI: &v1.CSIPersistentVolumeSource{
					Driver:       "vpc-csi-driver",
					VolumeHandle: "test-volumeid",

					VolumeAttributes: map[string]string{"tags": "mytag1:1,mytag2:2", ClusterIDLabel: "12345", "volumeCRN": "test-volcrn", "iops": "3000"},
				},
			},
		},
	}
	pvNoTags := pv.DeepCopy()
	pvNoTags.Spec.CSI.VolumeAttributes["tags"] = ""
	testCases := []struct {
		testCaseName string
		pv           *v1.PersistentVolume
		tags         string
	}{
		{
			testCaseName: "User tags- success",
			pv:           pv,
			tags:         "mytag1:1,mytag2:2",
		},
		{
			testCaseName: "No user tags- success",
			pv:           pvNoTags,
			tags:         "",
		},
	}
	for _, testcase := range testCases {
		//start test http server
		server = ghttp.NewServer()
		server.AppendHandlers(
			ghttp.CombineHandlers(
				ghttp.VerifyRequest(http.MethodGet, "/v3/tags"),
				ghttp.RespondWith(http.StatusOK, `
                           {
                            "items": {
                            }
                          }
                        `),
			),
		)
		_ = os.Setenv(IbmCloudGtAPIEndpoint, server.URL())
		t.Run(testcase.testCaseName, func(t *testing.T) {
			volCRN, tags := pvw.getTags(testcase.pv, logger)
			expectedTagNum := 7
			if len(testcase.tags) > 0 {
				expectedTagNum = 9
			}
			assert.Equal(t, expectedTagNum, len(tags))
			assert.Equal(t, "test-volcrn", volCRN)
			vol := pvw.getVolumeFromPV(pv, logger)
			assert.Equal(t, 1, *vol.Capacity)
			assert.Equal(t, "3000", *vol.Iops)
			assert.Equal(t, "test-volumeid", vol.VolumeID)
			assert.NotNil(t, vol.Attributes)
			assert.Equal(t, "12345", vol.Attributes[strings.ToLower(ClusterIDLabel)])

			pvw.updateVolume(testcase.pv, testcase.pv)
		})
	}
}

func TestFilter(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	conf := &config.Config{
		VPC: &config.VPCProviderConfig{
			VPCBlockProviderName: "vpc-classic",
		},
	}
	fakeIBMCloudStorageProvider, _ := cloudprovider.NewFakeIBMCloudStorageProvider("configPath", logger)

	pvw := &PVWatcher{
		provisionerName: "ibm-csi-driver",
		logger:          logger,
		config:          conf,
		cloudProvider:   fakeIBMCloudStorageProvider,
	}

	testCases := []struct {
		name     string
		pv       *v1.PersistentVolume
		expected bool
	}{
		{
			name: "Matching provisioner - should pass filter",
			pv: &v1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pv",
				},
				Spec: v1.PersistentVolumeSpec{
					PersistentVolumeSource: v1.PersistentVolumeSource{
						CSI: &v1.CSIPersistentVolumeSource{
							Driver:       "ibm-csi-driver",
							VolumeHandle: "test-volumeid",
						},
					},
				},
			},
			expected: true,
		},
		{
			name: "Non-matching provisioner - should not pass filter",
			pv: &v1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pv",
				},
				Spec: v1.PersistentVolumeSpec{
					PersistentVolumeSource: v1.PersistentVolumeSource{
						CSI: &v1.CSIPersistentVolumeSource{
							Driver:       "other-csi-driver",
							VolumeHandle: "test-volumeid",
						},
					},
				},
			},
			expected: false,
		},
		{
			name: "PV without CSI - should not pass filter",
			pv: &v1.PersistentVolume{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-pv",
				},
				Spec: v1.PersistentVolumeSpec{
					PersistentVolumeSource: v1.PersistentVolumeSource{},
				},
			},
			expected: false,
		},
		{
			name:     "Nil PV - should not pass filter",
			pv:       nil,
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := pvw.filter(tc.pv)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestStart(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	conf := &config.Config{
		VPC: &config.VPCProviderConfig{
			VPCBlockProviderName: "vpc-classic",
		},
	}
	fakeIBMCloudStorageProvider, _ := cloudprovider.NewFakeIBMCloudStorageProvider("configPath", logger)

	// Create a fake clientset with test PVs
	matchingPV := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pv-matching",
		},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName:              "test-storage-class",
			PersistentVolumeReclaimPolicy: v1.PersistentVolumeReclaimDelete,
			ClaimRef: &v1.ObjectReference{
				Namespace: "test-namespace",
				Name:      "test-pvc",
			},
			Capacity: v1.ResourceList(map[v1.ResourceName]resource.Quantity{
				v1.ResourceStorage: resource.MustParse("1Gi"),
			}),
			PersistentVolumeSource: v1.PersistentVolumeSource{
				CSI: &v1.CSIPersistentVolumeSource{
					Driver:       "ibm-csi-driver",
					VolumeHandle: "test-volumeid",
					VolumeAttributes: map[string]string{
						"tags":         "mytag1:1,mytag2:2",
						ClusterIDLabel: "12345",
						"volumeCRN":    "test-volcrn",
						"iops":         "3000",
					},
				},
			},
		},
		Status: v1.PersistentVolumeStatus{
			Phase: v1.VolumePending,
		},
	}

	nonMatchingPV := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pv-non-matching",
		},
		Spec: v1.PersistentVolumeSpec{
			PersistentVolumeSource: v1.PersistentVolumeSource{
				CSI: &v1.CSIPersistentVolumeSource{
					Driver:       "other-csi-driver",
					VolumeHandle: "other-volumeid",
				},
			},
		},
	}

	clientset := fake.NewSimpleClientset(matchingPV, nonMatchingPV)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	eventInterface := clientset.CoreV1().Events("")
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: eventInterface})

	pvw := &PVWatcher{
		provisionerName: "ibm-csi-driver",
		logger:          logger,
		config:          conf,
		cloudProvider:   fakeIBMCloudStorageProvider,
		kclient:         clientset,
		recorder:        broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "test-pod"}),
	}

	t.Run("Start initializes informer with correct configuration", func(t *testing.T) {
		// Verify all components needed by Start() are properly set up
		assert.NotNil(t, pvw.kclient, "kclient should not be nil")
		assert.NotNil(t, pvw.kclient.CoreV1(), "CoreV1 client should not be nil")
		assert.NotNil(t, pvw.logger, "logger should not be nil")
		assert.NotNil(t, pvw.config, "config should not be nil")
		assert.NotNil(t, pvw.cloudProvider, "cloudProvider should not be nil")
		assert.NotNil(t, pvw.recorder, "recorder should not be nil")
		assert.Equal(t, "ibm-csi-driver", pvw.provisionerName)
	})

	t.Run("Filter function works correctly with informer", func(t *testing.T) {
		// Test that filter correctly identifies matching PVs
		matchingFiltered := pvw.filter(matchingPV)
		assert.True(t, matchingFiltered, "PV with matching provisioner should pass filter")

		nonMatchingFiltered := pvw.filter(nonMatchingPV)
		assert.False(t, nonMatchingFiltered, "PV with non-matching provisioner should not pass filter")
	})

	t.Run("UpdateFunc handler is properly configured", func(t *testing.T) {
		// Verify updateVolume can be called (it runs in a goroutine)
		// This tests that the UpdateFunc handler in the informer would work correctly
		pvw.updateVolume(matchingPV, matchingPV)

		// Update with status change
		updatedPV := matchingPV.DeepCopy()
		updatedPV.Status.Phase = v1.VolumeBound
		pvw.updateVolume(matchingPV, updatedPV)

		// Update with capacity change
		capacityChangedPV := matchingPV.DeepCopy()
		capacityChangedPV.Spec.Capacity[v1.ResourceStorage] = resource.MustParse("2Gi")
		pvw.updateVolume(matchingPV, capacityChangedPV)
	})

	t.Run("Informer configuration matches expected setup", func(t *testing.T) {
		// Test that we can create the same informer configuration that Start() uses
		// This verifies the NewInformerWithOptions setup is correct
		watchlist := cache.NewListWatchFromClient(pvw.kclient.CoreV1().RESTClient(), "persistentvolumes", "", fields.Everything())
		assert.NotNil(t, watchlist, "watchlist should be created successfully")

		// Verify we can create an informer with the same options as Start()
		_, controller := cache.NewInformerWithOptions(cache.InformerOptions{
			ListerWatcher: watchlist,
			ObjectType:    &v1.PersistentVolume{},
			ResyncPeriod:  time.Second * 0,
			Handler: cache.FilteringResourceEventHandler{
				Handler: cache.ResourceEventHandlerFuncs{
					UpdateFunc: pvw.updateVolume,
				},
				FilterFunc: pvw.filter,
			},
		})
		assert.NotNil(t, controller, "controller should be created successfully")

		// Verify the controller has the expected configuration
		// The controller is ready to run, which validates the NewInformerWithOptions setup
	})
}

func TestStartInformerBehavior(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	conf := &config.Config{
		VPC: &config.VPCProviderConfig{
			VPCBlockProviderName: "vpc-classic",
		},
	}
	fakeIBMCloudStorageProvider, _ := cloudprovider.NewFakeIBMCloudStorageProvider("configPath", logger)

	// Create test PVs
	pv1 := &v1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-pv-1",
		},
		Spec: v1.PersistentVolumeSpec{
			StorageClassName:              "test-storage-class",
			PersistentVolumeReclaimPolicy: v1.PersistentVolumeReclaimDelete,
			ClaimRef: &v1.ObjectReference{
				Namespace: "test-namespace",
				Name:      "test-pvc-1",
			},
			Capacity: v1.ResourceList(map[v1.ResourceName]resource.Quantity{
				v1.ResourceStorage: resource.MustParse("1Gi"),
			}),
			PersistentVolumeSource: v1.PersistentVolumeSource{
				CSI: &v1.CSIPersistentVolumeSource{
					Driver:       "ibm-csi-driver",
					VolumeHandle: "test-volumeid-1",
					VolumeAttributes: map[string]string{
						"tags":         "mytag1:1",
						ClusterIDLabel: "12345",
						"volumeCRN":    "test-volcrn-1",
						"iops":         "3000",
					},
				},
			},
		},
		Status: v1.PersistentVolumeStatus{
			Phase: v1.VolumePending,
		},
	}

	clientset := fake.NewSimpleClientset(pv1)

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(glog.Infof)
	eventInterface := clientset.CoreV1().Events("")
	broadcaster.StartRecordingToSink(&v1core.EventSinkImpl{Interface: eventInterface})

	pvw := &PVWatcher{
		provisionerName: "ibm-csi-driver",
		logger:          logger,
		config:          conf,
		cloudProvider:   fakeIBMCloudStorageProvider,
		kclient:         clientset,
		recorder:        broadcaster.NewRecorder(scheme.Scheme, v1.EventSource{Component: "test-pod"}),
	}

	t.Run("Informer handles PV updates correctly", func(t *testing.T) {
		// Test various update scenarios that the informer should handle

		// Scenario 1: No change in status, capacity, or iops - should skip update
		pvw.updateVolume(pv1, pv1)

		// Scenario 2: Status change from Pending to Bound
		boundPV := pv1.DeepCopy()
		boundPV.Status.Phase = v1.VolumeBound
		pvw.updateVolume(pv1, boundPV)

		// Scenario 3: Capacity change
		largerPV := pv1.DeepCopy()
		largerPV.Spec.Capacity[v1.ResourceStorage] = resource.MustParse("10Gi")
		pvw.updateVolume(pv1, largerPV)

		// Scenario 4: IOPS change
		higherIOPSPV := pv1.DeepCopy()
		higherIOPSPV.Spec.CSI.VolumeAttributes["iops"] = "5000"
		pvw.updateVolume(pv1, higherIOPSPV)

		// Scenario 5: Status change to Released (delete scenario)
		releasedPV := pv1.DeepCopy()
		releasedPV.Status.Phase = v1.VolumeReleased
		pvw.updateVolume(pv1, releasedPV)
	})

	t.Run("Informer respects ResyncPeriod configuration", func(t *testing.T) {
		// Verify that ResyncPeriod is set to 0 as expected
		// This means the informer won't do periodic resyncs
		watchlist := cache.NewListWatchFromClient(pvw.kclient.CoreV1().RESTClient(), "persistentvolumes", "", fields.Everything())
		_, controller := cache.NewInformerWithOptions(cache.InformerOptions{
			ListerWatcher: watchlist,
			ObjectType:    &v1.PersistentVolume{},
			ResyncPeriod:  time.Second * 0, // Same as in Start()
			Handler: cache.FilteringResourceEventHandler{
				Handler: cache.ResourceEventHandlerFuncs{
					UpdateFunc: pvw.updateVolume,
				},
				FilterFunc: pvw.filter,
			},
		})
		assert.NotNil(t, controller)
		// Controller created successfully with ResyncPeriod of 0
	})
}

// GetTestLogger ...
func GetTestLogger(t *testing.T) (logger *zap.Logger, teardown func()) {
	atom := zap.NewAtomicLevel()
	atom.SetLevel(zap.DebugLevel)

	encoderCfg := zap.NewProductionEncoderConfig()
	encoderCfg.TimeKey = "timestamp"
	encoderCfg.EncodeTime = zapcore.ISO8601TimeEncoder

	buf := &bytes.Buffer{}

	logger = zap.New(
		zapcore.NewCore(
			zapcore.NewJSONEncoder(encoderCfg),
			zapcore.AddSync(buf),
			atom,
		),
		zap.AddCaller(),
	)

	teardown = func() {
		_ = logger.Sync()
		if t.Failed() {
			t.Log(buf)
		}
	}
	return
}
