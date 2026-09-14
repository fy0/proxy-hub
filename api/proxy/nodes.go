package proxy

import (
	"context"
	"time"

	"proxy-hub/api/h"
	"proxy-hub/model/tables"
	proxyService "proxy-hub/service/proxy"
	"proxy-hub/utils"
)

type nodeListInput struct {
	Page         int      `query:"page" validate:"omitempty,min=1"`
	Size         int      `query:"size" validate:"omitempty,min=1,max=200"`
	Keyword      string   `query:"keyword" validate:"omitempty"`
	NameOnly     bool     `query:"nameOnly"`
	GroupID      string   `query:"groupId" validate:"omitempty"`
	DefaultOnly  bool     `query:"defaultOnly"`
	PhysicalOnly bool     `query:"physicalOnly"`
	WithHealth   bool     `query:"withHealth" default:"true"`
	IDs          []string `query:"ids" explode:"false"`
}

type nodeListOutput struct {
	Body struct {
		Items []*proxyService.ProxyNodeDTO `json:"items"`
		Total int64                        `json:"total"`
		Page  int                          `json:"page"`
		Size  int                          `json:"size"`
	} `json:"body"`
}

func nodeListHandler(ctx context.Context, input *nodeListInput) (*nodeListOutput, error) {
	page := utils.GetPage(input.Page)
	size := utils.GetPageSize(input.Size, 50)
	if size > 200 {
		size = 200
	}
	nodes, total, err := proxyService.NodeListPaged(ctx, nil, proxyService.NodeListRequest{
		Keyword:      input.Keyword,
		NameOnly:     input.NameOnly,
		GroupID:      input.GroupID,
		DefaultOnly:  input.DefaultOnly,
		PhysicalOnly: input.PhysicalOnly,
		IDs:          input.IDs,
	}, page, size)
	if err != nil {
		return nil, mapError(err)
	}
	var healthByNodeID map[string]*tables.ProxyNodeHealthTable
	if input.WithHealth {
		healthByNodeID = proxyService.NodeHealthMap(ctx, nil, nodeIDsFromNodes(nodes))
	}
	groups, err := proxyService.GroupList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeListOutput{}
	output.Body.Items = proxyService.ToNodeDTOs(nodes, proxyService.NodeDTOOptions{HealthByNodeID: healthByNodeID, Groups: groups})
	output.Body.Total = total
	output.Body.Page = page
	output.Body.Size = size
	return output, nil
}

type nodeOptionsOutput struct {
	Body struct {
		Items []*proxyService.ProxyNodeOptionDTO `json:"items"`
		Total int64                              `json:"total"`
		Page  int                                `json:"page"`
		Size  int                                `json:"size"`
	} `json:"body"`
}

func nodeOptionsHandler(ctx context.Context, input *nodeListInput) (*nodeOptionsOutput, error) {
	page := utils.GetPage(input.Page)
	size := utils.GetPageSize(input.Size, 50)
	if size > 200 {
		size = 200
	}
	nodes, total, err := proxyService.NodeListPaged(ctx, nil, proxyService.NodeListRequest{
		Keyword:      input.Keyword,
		NameOnly:     input.NameOnly,
		GroupID:      input.GroupID,
		DefaultOnly:  input.DefaultOnly,
		PhysicalOnly: input.PhysicalOnly,
		IDs:          input.IDs,
	}, page, size)
	if err != nil {
		return nil, mapError(err)
	}
	groups, err := proxyService.GroupList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeOptionsOutput{}
	output.Body.Items = proxyService.ToNodeOptionDTOs(nodes, groups)
	output.Body.Total = total
	output.Body.Page = page
	output.Body.Size = size
	return output, nil
}

type nodeInput struct {
	Body proxyService.NodeUpsertRequest
}

type nodeOutput struct {
	Body struct {
		Item *proxyService.ProxyNodeDTO `json:"item"`
	} `json:"body"`
}

func nodeCreateHandler(ctx context.Context, input *nodeInput) (*nodeOutput, error) {
	node, err := proxyService.NodeCreate(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappingsForNodes(ctx, []string{node.ID}); err != nil {
		return nil, err
	}
	output := &nodeOutput{}
	output.Body.Item, err = nodeDTOWithGroups(ctx, node)
	if err != nil {
		return nil, err
	}
	return output, nil
}

type idInput struct {
	ID string `path:"id"`
}

type nodeUpdateInput struct {
	ID   string `path:"id"`
	Body proxyService.NodeUpsertRequest
}

func nodeUpdateHandler(ctx context.Context, input *nodeUpdateInput) (*nodeOutput, error) {
	affectedBefore, err := proxyService.RuntimeAffectedMappingIDsByNodes(ctx, []string{input.ID})
	if err != nil {
		return nil, mapError(err)
	}
	node, err := proxyService.NodeUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	affectedAfter, err := proxyService.RuntimeAffectedMappingIDsByNodes(ctx, []string{node.ID})
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(uniqueStrings(append(affectedBefore, affectedAfter...))); err != nil {
		return nil, err
	}
	output := &nodeOutput{}
	output.Body.Item, err = nodeDTOWithGroups(ctx, node)
	if err != nil {
		return nil, err
	}
	return output, nil
}

func nodeDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	affected, err := proxyService.RuntimeAffectedMappingIDsByNodes(ctx, []string{input.ID})
	if err != nil {
		return nil, mapError(err)
	}
	if err := proxyService.NodeDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(affected); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("节点已删除"), nil
}

type nodeImportInput struct {
	IncludeItems bool `query:"includeItems" default:"true"`
	Body         proxyService.NodeImportRequest
}

type nodeImportOutput struct {
	Body proxyService.NodeImportResult `json:"body"`
}

func nodeImportHandler(ctx context.Context, input *nodeImportInput) (*nodeImportOutput, error) {
	result, err := proxyService.NodeImport(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if result.Imported > 0 || result.Updated > 0 {
		if err := syncRuntimeMappingsForNodeDTOs(ctx, result.Items); err != nil {
			return nil, err
		}
		groupIDs := make([]string, 0, len(result.Groups))
		for _, group := range result.Groups {
			if group != nil {
				groupIDs = append(groupIDs, group.ID)
			}
		}
		if err := syncRuntimeMappingsForGroups(ctx, groupIDs); err != nil {
			return nil, err
		}
	}
	if !input.IncludeItems {
		result.Items = nil
		result.Groups = nil
	}
	return &nodeImportOutput{Body: *result}, nil
}

func nodeImportPreviewHandler(ctx context.Context, input *nodeImportInput) (*nodeImportOutput, error) {
	result, err := proxyService.NodeImportPreview(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &nodeImportOutput{Body: *result}, nil
}

type nodeHealthListOutput struct {
	Body struct {
		Items []*proxyService.ProxyNodeHealthDTO `json:"items"`
	} `json:"body"`
}

func nodeHealthListHandler(ctx context.Context, _ *struct{}) (*nodeHealthListOutput, error) {
	rows, err := proxyService.NodeHealthList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeHealthListOutput{}
	output.Body.Items = proxyService.ToNodeHealthDTOs(rows)
	return output, nil
}

type nodeHealthOutput struct {
	Body struct {
		Item *proxyService.ProxyNodeHealthDTO `json:"item"`
	} `json:"body"`
}

func nodeProbeHandler(ctx context.Context, input *idInput) (*nodeHealthOutput, error) {
	health, err := proxyService.NodeProbe(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeHealthOutput{}
	output.Body.Item = proxyService.ToNodeHealthDTO(health)
	return output, nil
}

type nodeProbeAllOutput struct {
	Body proxyService.NodeHealthProbeAllDTO `json:"body"`
}

func nodeProbeAllHandler(ctx context.Context, _ *struct{}) (*nodeProbeAllOutput, error) {
	result, err := proxyService.NodeProbeAll(ctx)
	if err != nil {
		return nil, mapError(err)
	}
	dto := proxyService.ToNodeHealthProbeAllDTO(result)
	return &nodeProbeAllOutput{Body: *dto}, nil
}

type nodeTestInput struct {
	ID   string `path:"id"`
	Body proxyService.ProxyTestRequest
}

type proxyTestOutput struct {
	Body proxyService.ProxyTestResultDTO `json:"body"`
}

func nodeTestHandler(ctx context.Context, input *nodeTestInput) (*proxyTestOutput, error) {
	result, err := proxyService.NodeTest(ctx, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &proxyTestOutput{Body: *result}, nil
}

func nodeReleaseHandler(ctx context.Context, input *idInput) (*nodeHealthOutput, error) {
	health, err := proxyService.NodeRelease(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeHealthOutput{}
	output.Body.Item = proxyService.ToNodeHealthDTO(health)
	return output, nil
}

type nodeBlacklistInput struct {
	ID   string `path:"id"`
	Body proxyService.NodeBlacklistRequest
}

func nodeBlacklistHandler(ctx context.Context, input *nodeBlacklistInput) (*nodeHealthOutput, error) {
	var duration time.Duration
	if input.Body.Duration != "" {
		parsed, err := time.ParseDuration(input.Body.Duration)
		if err != nil || parsed <= 0 {
			return nil, mapError(proxyService.ErrInvalidHealthDuration)
		}
		duration = parsed
	}
	health, err := proxyService.NodeBlacklist(ctx, input.ID, duration)
	if err != nil {
		return nil, mapError(err)
	}
	output := &nodeHealthOutput{}
	output.Body.Item = proxyService.ToNodeHealthDTO(health)
	return output, nil
}

func nodeIDsFromNodes(nodes []*tables.ProxyNodeTable) []string {
	ids := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if node != nil {
			ids = append(ids, node.ID)
		}
	}
	return ids
}

func nodeDTOWithGroups(ctx context.Context, node *tables.ProxyNodeTable) (*proxyService.ProxyNodeDTO, error) {
	groups, err := proxyService.GroupList(ctx, nil)
	if err != nil {
		return nil, err
	}
	return proxyService.ToNodeDTO(node, proxyService.NodeDTOOptions{Groups: groups}), nil
}
