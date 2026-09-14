package proxy

import (
	"context"

	"proxy-hub/api/h"
	proxyService "proxy-hub/service/proxy"
)

type groupListOutput struct {
	Body struct {
		Items []*proxyService.ProxyGroupDTO `json:"items"`
	} `json:"body"`
}

func groupListHandler(ctx context.Context, _ *struct{}) (*groupListOutput, error) {
	groups, err := proxyService.GroupList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &groupListOutput{}
	output.Body.Items = proxyService.ToGroupDTOs(groups)
	return output, nil
}

type groupInput struct {
	Body proxyService.GroupUpsertRequest
}

type groupOutput struct {
	Body struct {
		Item *proxyService.ProxyGroupDTO `json:"item"`
	} `json:"body"`
}

func groupCreateHandler(ctx context.Context, input *groupInput) (*groupOutput, error) {
	group, err := proxyService.GroupCreate(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappingsForGroups(ctx, []string{group.ID}); err != nil {
		return nil, err
	}
	output := &groupOutput{}
	output.Body.Item = proxyService.ToGroupDTO(group)
	return output, nil
}

type groupUpdateInput struct {
	ID   string `path:"id"`
	Body proxyService.GroupUpsertRequest
}

func groupUpdateHandler(ctx context.Context, input *groupUpdateInput) (*groupOutput, error) {
	affectedBefore, err := proxyService.RuntimeAffectedMappingIDsByGroups(ctx, []string{input.ID})
	if err != nil {
		return nil, mapError(err)
	}
	group, err := proxyService.GroupUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	affectedAfter, err := proxyService.RuntimeAffectedMappingIDsByGroups(ctx, []string{group.ID})
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(uniqueStrings(append(affectedBefore, affectedAfter...))); err != nil {
		return nil, err
	}
	output := &groupOutput{}
	output.Body.Item = proxyService.ToGroupDTO(group)
	return output, nil
}

func groupDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	affected, err := proxyService.RuntimeAffectedMappingIDsByGroups(ctx, []string{input.ID})
	if err != nil {
		return nil, mapError(err)
	}
	if err := proxyService.GroupDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(affected); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("节点组已删除"), nil
}
