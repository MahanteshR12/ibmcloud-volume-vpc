/**
 * Copyright 2026 IBM Corp.
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

package provider

import (
	"errors"
	"testing"
	"time"

	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	providerError "github.com/IBM/ibmcloud-volume-interface/lib/utils"
	"github.com/IBM/ibmcloud-volume-vpc/common/vpcclient/models"
	serviceFakes "github.com/IBM/ibmcloud-volume-vpc/common/vpcclient/vpcvolume/fakes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type fakeSnapshotConsistencyGroupManager struct {
	createRequest *models.SnapshotConsistencyGroupRequest
	createResult  *models.SnapshotConsistencyGroup
	createErr     error

	deleteGroupID string
	deleteErr     error

	getGroupID string
	getResult  *models.SnapshotConsistencyGroup
	getErr     error

	getByNameName            string
	getByNameResourceGroupID string
	getByNameResult          *models.SnapshotConsistencyGroup
	getByNameErr             error

	listFilters *models.ListSnapshotConsistencyGroupFilters
	listResult  *models.SnapshotConsistencyGroupList
	listErr     error
}

func (fake *fakeSnapshotConsistencyGroupManager) CreateSnapshotConsistencyGroup(req *models.SnapshotConsistencyGroupRequest, logger *zap.Logger) (*models.SnapshotConsistencyGroup, error) {
	fake.createRequest = req
	return fake.createResult, fake.createErr
}

func (fake *fakeSnapshotConsistencyGroupManager) DeleteSnapshotConsistencyGroup(groupID string, logger *zap.Logger) error {
	fake.deleteGroupID = groupID
	return fake.deleteErr
}

func (fake *fakeSnapshotConsistencyGroupManager) GetSnapshotConsistencyGroup(groupID string, logger *zap.Logger) (*models.SnapshotConsistencyGroup, error) {
	fake.getGroupID = groupID
	return fake.getResult, fake.getErr
}

func (fake *fakeSnapshotConsistencyGroupManager) GetSnapshotConsistencyGroupByName(name, resourceGroupID string, logger *zap.Logger) (*models.SnapshotConsistencyGroup, error) {
	fake.getByNameName = name
	fake.getByNameResourceGroupID = resourceGroupID
	return fake.getByNameResult, fake.getByNameErr
}

func (fake *fakeSnapshotConsistencyGroupManager) ListSnapshotConsistencyGroups(limit int, start string, filters *models.ListSnapshotConsistencyGroupFilters, logger *zap.Logger) (*models.SnapshotConsistencyGroupList, error) {
	fake.listFilters = filters
	return fake.listResult, fake.listErr
}

func TestCreateGroupSnapshotCreatesConsistencyGroupAndListsMembers(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	vpcs, uc, _, err := GetTestOpenSession(t, logger)
	require.NoError(t, err)

	now := time.Now()
	groupService := &fakeSnapshotConsistencyGroupManager{
		createResult: &models.SnapshotConsistencyGroup{
			ID:             "group-snapshot-id",
			CRN:            "group-snapshot-crn",
			Href:           "group-snapshot-href",
			LifecycleState: snapshotReadyState,
			CreatedAt:      &now,
			// Match the VPC create response: member references precede full details.
			Snapshots: []models.SnapshotReference{
				{ID: "snapshot-id-1", CRN: "snapshot-crn-1"},
				{ID: "snapshot-id-2", CRN: "snapshot-crn-2"},
			},
		},
	}
	snapshotService := &serviceFakes.SnapshotManager{}
	snapshotService.ListSnapshotsReturns(&models.SnapshotList{
		Snapshots: []*models.Snapshot{
			{
				ID:              "snapshot-id-1",
				CRN:             "snapshot-crn-1",
				MinimumCapacity: 10,
				LifecycleState:  snapshotReadyState,
				SourceVolume:    &models.SourceVolume{ID: "volume-id-1"},
			},
			{
				ID:              "snapshot-id-2",
				CRN:             "snapshot-crn-2",
				MinimumCapacity: 20,
				LifecycleState:  snapshotReadyState,
				SourceVolume:    &models.SourceVolume{ID: "volume-id-2"},
			},
		},
	}, nil)
	uc.SnapshotConsistencyGroupServiceReturns(groupService)
	uc.SnapshotServiceReturns(snapshotService)

	groupSnapshot, err := vpcs.CreateGroupSnapshot([]string{"volume-id-1", "volume-id-2"}, provider.GroupSnapshotParameters{
		Name:          "group-snapshot-name",
		ResourceGroup: "resource-group-id",
	})

	require.NoError(t, err)
	require.NotNil(t, groupSnapshot)
	assert.Equal(t, "group-snapshot-id", groupSnapshot.GroupSnapshotID)
	assert.Equal(t, "group-snapshot-crn", groupSnapshot.GroupSnapshotCRN)
	assert.True(t, groupSnapshot.ReadyToUse)
	assert.Len(t, groupSnapshot.Snapshots, 2)
	assert.Equal(t, "snapshot-id-1", groupSnapshot.Snapshots[0].SnapshotID)
	assert.Equal(t, "volume-id-1", groupSnapshot.Snapshots[0].VolumeID)
	assert.Equal(t, "group-snapshot-name", groupService.createRequest.Name)
	assert.Equal(t, "resource-group-id", groupService.createRequest.ResourceGroup.ID)
	assert.True(t, groupService.createRequest.DeleteSnapshotsOnDelete)
	require.Len(t, groupService.createRequest.Snapshots, 2)
	assert.Equal(t, "volume-id-1", groupService.createRequest.Snapshots[0].SourceVolume.ID)
	assert.Equal(t, "volume-id-2", groupService.createRequest.Snapshots[1].SourceVolume.ID)
	_, _, filters, _ := snapshotService.ListSnapshotsArgsForCall(0)
	assert.Equal(t, "group-snapshot-id", filters.SnapshotConsistencyGroupID)
}

func TestDeleteGroupSnapshotDeletesConsistencyGroupByID(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	vpcs, uc, _, err := GetTestOpenSession(t, logger)
	require.NoError(t, err)

	groupService := &fakeSnapshotConsistencyGroupManager{}
	uc.SnapshotConsistencyGroupServiceReturns(groupService)

	err = vpcs.DeleteGroupSnapshot("group-snapshot-id", []string{"snapshot-id-1", "snapshot-id-2"})

	require.NoError(t, err)
	assert.Equal(t, "group-snapshot-id", groupService.deleteGroupID)
}

func TestGetGroupSnapshotGetsConsistencyGroupAndListsMembers(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	vpcs, uc, _, err := GetTestOpenSession(t, logger)
	require.NoError(t, err)

	groupService := &fakeSnapshotConsistencyGroupManager{
		getResult: &models.SnapshotConsistencyGroup{
			ID:             "group-snapshot-id",
			CRN:            "group-snapshot-crn",
			LifecycleState: snapshotReadyState,
			Snapshots: []models.SnapshotReference{
				{ID: "snapshot-id-1", CRN: "snapshot-crn-1"},
			},
		},
	}
	snapshotService := &serviceFakes.SnapshotManager{}
	snapshotService.ListSnapshotsReturns(&models.SnapshotList{
		Snapshots: []*models.Snapshot{
			{
				ID:              "snapshot-id-1",
				CRN:             "snapshot-crn-1",
				MinimumCapacity: 10,
				LifecycleState:  snapshotReadyState,
				SourceVolume:    &models.SourceVolume{ID: "volume-id-1"},
			},
		},
	}, nil)
	uc.SnapshotConsistencyGroupServiceReturns(groupService)
	uc.SnapshotServiceReturns(snapshotService)

	groupSnapshot, err := vpcs.GetGroupSnapshot("group-snapshot-id")

	require.NoError(t, err)
	require.NotNil(t, groupSnapshot)
	assert.Equal(t, "group-snapshot-id", groupService.getGroupID)
	assert.Equal(t, "group-snapshot-id", groupSnapshot.GroupSnapshotID)
	assert.True(t, groupSnapshot.ReadyToUse)
	require.Len(t, groupSnapshot.Snapshots, 1)
	assert.Equal(t, "volume-id-1", groupSnapshot.Snapshots[0].VolumeID)
	_, _, filters, _ := snapshotService.ListSnapshotsArgsForCall(0)
	assert.Equal(t, "group-snapshot-id", filters.SnapshotConsistencyGroupID)
}

func TestGroupSnapshotMemberLookup(t *testing.T) {
	operations := []struct {
		name string
		call func(*VPCSession) (*provider.GroupSnapshot, error)
	}{
		{
			name: "create",
			call: func(session *VPCSession) (*provider.GroupSnapshot, error) {
				return session.CreateGroupSnapshot([]string{"volume-id-1", "volume-id-2"}, provider.GroupSnapshotParameters{
					Name: "group-snapshot-name", ResourceGroup: "resource-group-id",
				})
			},
		},
		{
			name: "get by ID",
			call: func(session *VPCSession) (*provider.GroupSnapshot, error) {
				return session.GetGroupSnapshot("group-snapshot-id")
			},
		},
		{
			name: "get by name",
			call: func(session *VPCSession) (*provider.GroupSnapshot, error) {
				return session.GetGroupSnapshotByName("group-snapshot-name", "resource-group-id")
			},
		},
	}
	fullMemberDetails := &models.SnapshotList{Snapshots: []*models.Snapshot{
		{ID: "snapshot-id-1", CRN: "snapshot-crn-1", LifecycleState: snapshotReadyState, SourceVolume: &models.SourceVolume{ID: "volume-id-1"}},
		{ID: "snapshot-id-2", CRN: "snapshot-crn-2", LifecycleState: snapshotReadyState, SourceVolume: &models.SourceVolume{ID: "volume-id-2"}},
	}}
	memberLookups := []struct {
		name      string
		result    *models.SnapshotList
		err       error
		wantReady bool
	}{
		{
			name:      "full details",
			result:    fullMemberDetails,
			wantReady: true,
		},
		{
			name: "service unavailable",
			err: &models.Error{Trace: "member-lookup-trace", Errors: []models.ErrorItem{
				{Code: models.ErrorCode("snapshots_service_unavailable"), Status: "503 Service Unavailable"},
			}},
		},
		{
			name: "permission denied",
			err: &models.Error{Trace: "member-lookup-trace", Errors: []models.ErrorItem{
				{Code: models.ErrorCode("snapshots_not_authorized"), Status: "403 Forbidden"},
			}},
		},
		{
			name: "authentication failed",
			err: &models.Error{Trace: "member-lookup-trace", Errors: []models.ErrorItem{
				{Code: models.ErrorCodeTokenInvalid, Status: "401 Unauthorized"},
			}},
		},
		{
			name: "rate limited",
			err: &models.Error{Trace: "member-lookup-trace", Errors: []models.ErrorItem{
				{Code: models.ErrorCode("snapshots_too_many_requests"), Status: "429 Too Many Requests"},
			}},
		},
		{
			name: "internal backend error",
			err: &models.Error{Trace: "member-lookup-trace", Errors: []models.ErrorItem{
				{Code: models.ErrorCode("internal_error"), Status: "500 Internal Server Error"},
			}},
		},
		{name: "transport error", err: errors.New("snapshot endpoint connection reset")},
		{name: "no details returned"},
		{name: "empty details returned", result: &models.SnapshotList{}},
		{name: "partial details returned", result: &models.SnapshotList{Snapshots: fullMemberDetails.Snapshots[:1]}},
	}

	for _, operation := range operations {
		for _, lookup := range memberLookups {
			t.Run(operation.name+"/"+lookup.name, func(t *testing.T) {
				logger, teardown := GetTestLogger(t)
				defer teardown()
				session, client, _, err := GetTestOpenSession(t, logger)
				require.NoError(t, err)

				group := &models.SnapshotConsistencyGroup{
					ID: "group-snapshot-id", LifecycleState: snapshotReadyState,
					Snapshots: []models.SnapshotReference{
						{ID: "snapshot-id-1", CRN: "snapshot-crn-1"},
						{ID: "snapshot-id-2", CRN: "snapshot-crn-2"},
					},
				}
				client.SnapshotConsistencyGroupServiceReturns(&fakeSnapshotConsistencyGroupManager{
					createResult: group, getResult: group, getByNameResult: group,
				})
				snapshotService := &serviceFakes.SnapshotManager{}
				snapshotService.ListSnapshotsReturns(lookup.result, lookup.err)
				client.SnapshotServiceReturns(snapshotService)

				response, err := operation.call(session)

				// Failures must leave this request after one member lookup, without an internal retry loop.
				require.Equal(t, 1, snapshotService.ListSnapshotsCallCount())
				_, _, filters, _ := snapshotService.ListSnapshotsArgsForCall(0)
				assert.Equal(t, group.ID, filters.SnapshotConsistencyGroupID)
				if lookup.err != nil {
					require.Error(t, err)
					assert.Nil(t, response)
					var memberLookupError providerError.Message
					require.ErrorAs(t, err, &memberLookupError)
					assert.Equal(t, "GroupSnapshotMemberLookupFailed", memberLookupError.Code)
					assert.Equal(t, lookup.err.Error(), memberLookupError.BackendError)
					assert.Contains(t, memberLookupError.Description, group.ID)

					// Once the backend recovers, the next request can read the same group successfully.
					snapshotService.ListSnapshotsReturns(fullMemberDetails, nil)
					response, err = session.GetGroupSnapshot(group.ID)
					require.NoError(t, err)
					require.NotNil(t, response)
					assert.Equal(t, group.ID, response.GroupSnapshotID)
					assert.True(t, response.ReadyToUse)
					assert.Equal(t, 2, snapshotService.ListSnapshotsCallCount())
					return
				}

				require.NoError(t, err)
				require.NotNil(t, response)
				assert.Equal(t, group.ID, response.GroupSnapshotID)
				assert.Equal(t, lookup.wantReady, response.ReadyToUse)
				require.Len(t, response.Snapshots, 2)
				assert.Equal(t, "snapshot-id-1", response.Snapshots[0].SnapshotID)
				assert.Equal(t, "snapshot-id-2", response.Snapshots[1].SnapshotID)
				if lookup.wantReady {
					assert.Equal(t, "volume-id-1", response.Snapshots[0].VolumeID)
					assert.Equal(t, "volume-id-2", response.Snapshots[1].VolumeID)
				} else if lookup.result == nil || len(lookup.result.Snapshots) == 0 {
					assert.Empty(t, response.Snapshots[0].VolumeID)
					assert.Empty(t, response.Snapshots[1].VolumeID)
				} else {
					assert.Equal(t, "volume-id-1", response.Snapshots[0].VolumeID)
					assert.Empty(t, response.Snapshots[1].VolumeID)
				}
			})
		}
	}
}

func TestFromProviderToLibGroupSnapshotReadiness(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	testCases := []struct {
		name            string
		groupState      string
		groupMembers    []models.SnapshotReference
		snapshotDetails []*models.Snapshot
		expectedReady   bool
	}{
		{
			name:       "stable group with all stable members",
			groupState: snapshotReadyState,
			groupMembers: []models.SnapshotReference{
				{ID: "snapshot-id-1"},
				{ID: "snapshot-id-2"},
			},
			snapshotDetails: []*models.Snapshot{
				{ID: "snapshot-id-1", LifecycleState: snapshotReadyState, SourceVolume: &models.SourceVolume{ID: "volume-id-1"}},
				{ID: "snapshot-id-2", LifecycleState: snapshotReadyState, SourceVolume: &models.SourceVolume{ID: "volume-id-2"}},
			},
			expectedReady: true,
		},
		{
			name:       "stable group with a pending member",
			groupState: snapshotReadyState,
			groupMembers: []models.SnapshotReference{
				{ID: "snapshot-id-1"},
				{ID: "snapshot-id-2"},
			},
			snapshotDetails: []*models.Snapshot{
				{ID: "snapshot-id-1", LifecycleState: snapshotReadyState, SourceVolume: &models.SourceVolume{ID: "volume-id-1"}},
				{ID: "snapshot-id-2", LifecycleState: "pending", SourceVolume: &models.SourceVolume{ID: "volume-id-2"}},
			},
			expectedReady: false,
		},
		{
			name:       "pending group with stable members",
			groupState: "pending",
			groupMembers: []models.SnapshotReference{
				{ID: "snapshot-id-1"},
			},
			snapshotDetails: []*models.Snapshot{
				{ID: "snapshot-id-1", LifecycleState: snapshotReadyState, SourceVolume: &models.SourceVolume{ID: "volume-id-1"}},
			},
			expectedReady: false,
		},
		{
			name:       "stable group without full member details",
			groupState: snapshotReadyState,
			groupMembers: []models.SnapshotReference{
				{ID: "snapshot-id-1"},
			},
			snapshotDetails: nil,
			expectedReady:   false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			group := &models.SnapshotConsistencyGroup{
				ID:             "group-snapshot-id",
				LifecycleState: tc.groupState,
				Snapshots:      tc.groupMembers,
			}

			result := FromProviderToLibGroupSnapshot(group, tc.snapshotDetails, logger)

			require.NotNil(t, result)
			assert.Equal(t, tc.expectedReady, result.ReadyToUse)
		})
	}
}

// Partial member details must keep the group not ready until all members are available.
func TestFromProviderToLibGroupSnapshotPartialMembers(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	group := &models.SnapshotConsistencyGroup{
		ID:             "group-snapshot-id",
		LifecycleState: snapshotReadyState,
		Snapshots: []models.SnapshotReference{
			{ID: "snapshot-id-1", CRN: "snapshot-crn-1"},
			{ID: "snapshot-id-2", CRN: "snapshot-crn-2"},
		},
	}
	// Only one of the two members has full details available.
	snapshotDetails := []*models.Snapshot{
		{
			ID:             "snapshot-id-1",
			CRN:            "snapshot-crn-1",
			LifecycleState: snapshotReadyState,
			SourceVolume:   &models.SourceVolume{ID: "volume-id-1"},
		},
	}

	result := FromProviderToLibGroupSnapshot(group, snapshotDetails, logger)

	require.NotNil(t, result)
	require.Len(t, result.Snapshots, 2)
	assert.Equal(t, "volume-id-1", result.Snapshots[0].VolumeID)
	assert.Equal(t, "snapshot-crn-1", result.Snapshots[0].SnapshotCRN)
	assert.True(t, result.Snapshots[0].ReadyToUse)
	assert.Equal(t, "snapshot-id-2", result.Snapshots[1].SnapshotID)
	assert.Empty(t, result.Snapshots[1].VolumeID)
	assert.Equal(t, "snapshot-crn-2", result.Snapshots[1].SnapshotCRN)
	assert.False(t, result.Snapshots[1].ReadyToUse)
	assert.False(t, result.ReadyToUse)

	// A later poll supplies the missing details and makes the same group ready.
	snapshotDetails = append(snapshotDetails, &models.Snapshot{
		ID:             "snapshot-id-2",
		CRN:            "snapshot-crn-2",
		LifecycleState: snapshotReadyState,
		SourceVolume:   &models.SourceVolume{ID: "volume-id-2"},
	})
	result = FromProviderToLibGroupSnapshot(group, snapshotDetails, logger)
	require.NotNil(t, result)
	require.Len(t, result.Snapshots, 2)
	assert.Equal(t, "volume-id-2", result.Snapshots[1].VolumeID)
	assert.True(t, result.Snapshots[1].ReadyToUse)
	assert.True(t, result.ReadyToUse)
}

// Missing source-volume details must not panic or produce a ready snapshot.
func TestFromProviderToLibSnapshotNilSourceVolume(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	for _, tc := range []struct {
		name         string
		sourceVolume *models.SourceVolume
	}{
		{name: "nil source volume"},
		{name: "empty source volume ID", sourceVolume: &models.SourceVolume{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := FromProviderToLibSnapshot(&models.Snapshot{
				ID:             "snapshot-id-1",
				CRN:            "snapshot-crn-1",
				LifecycleState: snapshotReadyState,
				SourceVolume:   tc.sourceVolume,
			}, logger)

			require.NotNil(t, result)
			assert.Empty(t, result.VolumeID)
			assert.False(t, result.ReadyToUse)
		})
	}
}

// Deleting an already-missing group must return its backend error after one attempt.
func TestGroupNotFoundSkipsRetry(t *testing.T) {
	logger, teardown := GetTestLogger(t)
	defer teardown()

	// Allow an unwanted retry to be detected without leaking settings to other tests.
	previousAttempts, previousGap := maxRetryAttempt, maxRetryGap
	t.Cleanup(func() {
		SetRetryParameters(previousAttempts, previousGap)
	})
	SetRetryParameters(2, 1)

	notFoundErr := &models.Error{
		Errors: []models.ErrorItem{
			{Code: models.ErrorCode("snapshot_consistency_groups_not_found")},
		},
	}

	attempts := 0
	err := retry(logger, func() error {
		attempts++
		return notFoundErr
	})

	assert.Equal(t, 1, attempts, "retry must stop after the first attempt for a skip-listed error code")
	assert.Same(t, notFoundErr, err)
}
