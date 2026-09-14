package proxy

import (
	"context"

	"proxy-hub/api/h"
	proxyService "proxy-hub/service/proxy"
)

type subscriptionListOutput struct {
	Body struct {
		Items []*proxyService.ProxySubscriptionDTO `json:"items"`
	} `json:"body"`
}

func subscriptionListHandler(ctx context.Context, _ *struct{}) (*subscriptionListOutput, error) {
	subscriptions, err := proxyService.SubscriptionList(ctx, nil)
	if err != nil {
		return nil, mapError(err)
	}
	output := &subscriptionListOutput{}
	output.Body.Items = proxyService.ToSubscriptionDTOs(subscriptions)
	return output, nil
}

type subscriptionInput struct {
	Body proxyService.SubscriptionUpsertRequest
}

type subscriptionOutput struct {
	Body struct {
		Item *proxyService.ProxySubscriptionDTO `json:"item"`
	} `json:"body"`
}

type subscriptionPreviewInput struct {
	Body proxyService.SubscriptionUpsertRequest
}

type subscriptionPreviewOutput struct {
	Body proxyService.NodeImportResult `json:"body"`
}

func subscriptionPreviewHandler(ctx context.Context, input *subscriptionPreviewInput) (*subscriptionPreviewOutput, error) {
	result, err := proxyService.SubscriptionPreview(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	return &subscriptionPreviewOutput{Body: *result}, nil
}

func subscriptionCreateHandler(ctx context.Context, input *subscriptionInput) (*subscriptionOutput, error) {
	subscription, err := proxyService.SubscriptionCreate(ctx, nil, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	output := &subscriptionOutput{}
	output.Body.Item = proxyService.ToSubscriptionDTO(subscription)
	return output, nil
}

type subscriptionUpdateInput struct {
	ID   string `path:"id"`
	Body proxyService.SubscriptionUpsertRequest
}

func subscriptionUpdateHandler(ctx context.Context, input *subscriptionUpdateInput) (*subscriptionOutput, error) {
	subscription, err := proxyService.SubscriptionUpdate(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	output := &subscriptionOutput{}
	output.Body.Item = proxyService.ToSubscriptionDTO(subscription)
	return output, nil
}

func subscriptionDeleteHandler(ctx context.Context, input *idInput) (*h.MessageResponse, error) {
	affected, err := proxyService.RuntimeAffectedMappingIDsBySubscription(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	if err := proxyService.SubscriptionDelete(ctx, nil, input.ID); err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(affected); err != nil {
		return nil, err
	}
	return h.NewMessageResponse("订阅已删除"), nil
}

type subscriptionSyncInput struct {
	ID           string `path:"id"`
	IncludeItems bool   `query:"includeItems" default:"true"`
	Body         proxyService.SubscriptionSyncRequest
}

type subscriptionSyncOutput struct {
	Body proxyService.NodeImportResult `json:"body"`
}

func subscriptionSyncHandler(ctx context.Context, input *subscriptionSyncInput) (*subscriptionSyncOutput, error) {
	affectedBefore, err := proxyService.RuntimeAffectedMappingIDsBySubscription(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	result, err := proxyService.SubscriptionSync(ctx, nil, input.ID, input.Body)
	if err != nil {
		return nil, mapError(err)
	}
	affectedAfter, err := proxyService.RuntimeAffectedMappingIDsBySubscription(ctx, input.ID)
	if err != nil {
		return nil, mapError(err)
	}
	if err := syncRuntimeMappings(uniqueStrings(append(affectedBefore, affectedAfter...))); err != nil {
		return nil, err
	}
	if !input.IncludeItems {
		result.Items = nil
		result.Groups = nil
	}
	return &subscriptionSyncOutput{Body: *result}, nil
}
