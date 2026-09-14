package proxy

import (
	"context"

	"proxy-hub/api/h"
	proxyService "proxy-hub/service/proxy"
)

type mappingListOutput struct {
	Body struct {
		Items []*proxyService.PortMappingDTO `json:"items"`
	} `json:"body"`
}

func mappingListHandler(ctx context.Context, _ *struct{}) (*mappingListOutput, error) {
	mappings, err := proxyService.MappingList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &mappingListOutput{}
	output.Body.Items = proxyService.ToMappingDTOs(mappings)
	return output, nil
}

type mappingInput struct {
	Body proxyService.MappingUpsertRequest
}

type mappingOutput struct {
	Body struct {
		Item *proxyService.PortMappingDTO `json:"item"`
	} `json:"body"`
}

func mappingCreateHandler(ctx context.Context, input *mappingInput) (*mappingOutput, error) {
	mapping, err := proxyService.MappingCreate(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMapping(ctx, mapping.ID); err != nil {
		return nil, err
	}
	output := &mappingOutput{}
	output.Body.Item = proxyService.ToMappingDTO(mapping)
	return output, nil
}

type mappingUpdateInput struct {
	ID   string `path:"id"`
	Body proxyService.MappingUpsertRequest
}

func mappingUpdateHandler(ctx context.Context, input *mappingUpdateInput) (*mappingOutput, error) {
	mapping, err := proxyService.MappingUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMapping(ctx, mapping.ID); err != nil {
		return nil, err
	}
	output := &mappingOutput{}
	output.Body.Item = proxyService.ToMappingDTO(mapping)
	return output, nil
}

func mappingDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	if err := proxyService.MappingDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if _, err := proxyService.RuntimeRemoveMapping(input.ID); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("端口映射已删除"), nil
}

type mappingTestInput struct {
	ID   string `path:"id"`
	Body proxyService.ProxyTestRequest
}

func mappingTestHandler(ctx context.Context, input *mappingTestInput) (*proxyTestOutput, error) {
	result, err := proxyService.MappingTest(ctx, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &proxyTestOutput{Body: *result}, nil
}

type mappingSwitchInput struct {
	ID   string `path:"id"`
	Body proxyService.MappingSwitchRequest
}

func mappingSwitchHandler(ctx context.Context, input *mappingSwitchInput) (*mappingOutput, error) {
	mapping, err := proxyService.MappingSwitch(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMapping(ctx, mapping.ID); err != nil {
		return nil, err
	}
	output := &mappingOutput{}
	output.Body.Item = proxyService.ToMappingDTO(mapping)
	return output, nil
}
