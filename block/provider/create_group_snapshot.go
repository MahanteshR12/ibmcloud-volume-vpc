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
	"time"

	"github.com/IBM/ibmcloud-volume-interface/lib/metrics"
	"github.com/IBM/ibmcloud-volume-interface/lib/provider"
	userError "github.com/IBM/ibmcloud-volume-vpc/common/messages"
	"github.com/IBM/ibmcloud-volume-vpc/common/vpcclient/models"
	"go.uber.org/zap"
)

// CreateGroupSnapshot creates a snapshot consistency group for the given source volume IDs
func (vpcs *VPCSession) CreateGroupSnapshot(sourceVolumeIDs []string, groupSnapshotParameters provider.GroupSnapshotParameters) (*provider.GroupSnapshot, error) {
	vpcs.Logger.Info("Entry CreateGroupSnapshot", zap.Reflect("groupSnapshotParameters", groupSnapshotParameters), zap.Reflect("sourceVolumeIDs", sourceVolumeIDs))
	defer vpcs.Logger.Info("Exit CreateGroupSnapshot", zap.Reflect("groupSnapshotParameters", groupSnapshotParameters), zap.Reflect("sourceVolumeIDs", sourceVolumeIDs))
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "CreateGroupSnapshot", time.Now())

	// Build the snapshot templates for each source volume
	snapshotTemplates := make([]models.GroupSnapshotTemplate, 0, len(sourceVolumeIDs))
	for _, volID := range sourceVolumeIDs {
		snapshotTemplates = append(snapshotTemplates, models.GroupSnapshotTemplate{
			SourceVolume: &models.SourceVolume{
				ID: volID,
			},
		})
	}

	reqBody := &models.SnapshotConsistencyGroupRequest{
		Name: groupSnapshotParameters.Name,
		ResourceGroup: &models.ResourceGroup{
			ID: groupSnapshotParameters.ResourceGroup,
		},
		Snapshots:               snapshotTemplates,
		DeleteSnapshotsOnDelete: true,
	}

	var result *models.SnapshotConsistencyGroup
	var err error
	err = retry(vpcs.Logger, func() error {
		result, err = vpcs.Apiclient.SnapshotConsistencyGroupService().CreateSnapshotConsistencyGroup(reqBody, vpcs.Logger)
		return err
	})
	if err != nil {
		return nil, userError.GetUserError("FailedToCreateGroupSnapshot", err)
	}

	vpcs.Logger.Info("Successfully created snapshot consistency group", zap.Reflect("GroupSnapshot", result))

	groupSnapshotResponse, err := vpcs.getGroupSnapshotWithMembers(result)
	if err != nil {
		return nil, err
	}
	vpcs.Logger.Info("Prepared volume group snapshot response", zap.Reflect("groupSnapshotResponse", groupSnapshotResponse))
	return groupSnapshotResponse, nil
}

// DeleteGroupSnapshot deletes a snapshot consistency group
// snapshotIDs contains the list of individual snapshot IDs within the group snapshot (required by CSI spec)
func (vpcs *VPCSession) DeleteGroupSnapshot(groupSnapshotID string, snapshotIDs []string) error {
	vpcs.Logger.Info("Entry DeleteGroupSnapshot", zap.Reflect("groupSnapshotID", groupSnapshotID), zap.Reflect("snapshotIDs", snapshotIDs))
	defer vpcs.Logger.Info("Exit DeleteGroupSnapshot", zap.Reflect("groupSnapshotID", groupSnapshotID))
	defer metrics.UpdateDurationFromStart(vpcs.Logger, "DeleteGroupSnapshot", time.Now())

	err := retry(vpcs.Logger, func() error {
		return vpcs.Apiclient.SnapshotConsistencyGroupService().DeleteSnapshotConsistencyGroup(groupSnapshotID, vpcs.Logger)
	})
	if err != nil {
		return userError.GetUserError("FailedToDeleteGroupSnapshot", err, groupSnapshotID)
	}

	vpcs.Logger.Info("Successfully deleted the snapshot consistency group")
	return nil
}

// GetGroupSnapshot gets a snapshot consistency group by ID
func (vpcs *VPCSession) GetGroupSnapshot(groupSnapshotID string) (*provider.GroupSnapshot, error) {
	vpcs.Logger.Info("Entry GetGroupSnapshot", zap.Reflect("groupSnapshotID", groupSnapshotID))
	defer vpcs.Logger.Info("Exit GetGroupSnapshot", zap.Reflect("groupSnapshotID", groupSnapshotID))

	var result *models.SnapshotConsistencyGroup
	var err error
	err = retry(vpcs.Logger, func() error {
		result, err = vpcs.Apiclient.SnapshotConsistencyGroupService().GetSnapshotConsistencyGroup(groupSnapshotID, vpcs.Logger)
		return err
	})
	if err != nil {
		return nil, userError.GetUserError("GroupSnapshotRetrievalFailed", err, groupSnapshotID)
	}

	vpcs.Logger.Info("Successfully retrieved snapshot consistency group details", zap.Reflect("groupSnapshotDetails", result))

	return vpcs.getGroupSnapshotWithMembers(result)
}

// GetGroupSnapshotByName gets a snapshot consistency group by name
func (vpcs *VPCSession) GetGroupSnapshotByName(name string, resourceGroupID string) (*provider.GroupSnapshot, error) {
	vpcs.Logger.Debug("Entry of GetGroupSnapshotByName method...")
	defer vpcs.Logger.Debug("Exit from GetGroupSnapshotByName method...")

	vpcs.Logger.Info("Retrieving snapshot consistency group by name from VPC", zap.Reflect("GroupSnapshotName", name))

	if len(name) == 0 {
		return nil, userError.GetUserError("InvalidGroupSnapshotName", nil)
	}

	var result *models.SnapshotConsistencyGroup
	var err error
	err = retry(vpcs.Logger, func() error {
		result, err = vpcs.Apiclient.SnapshotConsistencyGroupService().GetSnapshotConsistencyGroupByName(name, resourceGroupID, vpcs.Logger)
		return err
	})
	if err != nil {
		return nil, userError.GetUserError("GroupSnapshotNameLookupFailed", err, name)
	}

	if result == nil {
		return nil, nil
	}

	vpcs.Logger.Info("Successfully retrieved snapshot consistency group details", zap.Reflect("groupSnapshotDetails", result))

	return vpcs.getGroupSnapshotWithMembers(result)
}

// getGroupSnapshotWithMembers fetches full member details and converts the group.
// Return backend errors immediately so the snapshotter can schedule retries.
func (vpcs *VPCSession) getGroupSnapshotWithMembers(group *models.SnapshotConsistencyGroup) (*provider.GroupSnapshot, error) {
	snapshotList, err := vpcs.Apiclient.SnapshotService().ListSnapshots(0, "", &models.LisSnapshotFilters{
		SnapshotConsistencyGroupID: group.ID,
	}, vpcs.Logger)
	if err != nil {
		vpcs.Logger.Error("Failed to retrieve individual member snapshot details",
			zap.String("groupSnapshotID", group.ID), zap.Error(err))
		return nil, userError.GetUserError("GroupSnapshotMemberLookupFailed", err, group.ID)
	}

	// A successful read can still contain incomplete details while members are being created.
	var snapshotDetails []*models.Snapshot
	if snapshotList != nil {
		snapshotDetails = snapshotList.Snapshots
	}

	return FromProviderToLibGroupSnapshot(group, snapshotDetails, vpcs.Logger), nil
}
