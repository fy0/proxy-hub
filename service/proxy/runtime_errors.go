package proxy

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/sagernet/sing-box/option"

	"proxy-hub/model/tables"
	"proxy-hub/service/proxyuri"
)

type nodeBuildError struct {
	node *tables.ProxyNodeTable
	err  error
}

type dynamicMemberError struct {
	member dynamicMemberPlan
	err    error
}

func (err nodeBuildError) Error() string {
	if err.err == nil {
		return ""
	}
	if err.node == nil {
		return err.err.Error()
	}
	return fmt.Sprintf("节点 %s 配置无效: %v", err.node.Name, err.err)
}

func (err nodeBuildError) Unwrap() error {
	return err.err
}

func asNodeBuildError(err error) (nodeBuildError, bool) {
	var buildErr nodeBuildError
	if errors.As(err, &buildErr) && buildErr.node != nil {
		return buildErr, true
	}
	return nodeBuildError{}, false
}

func (err dynamicMemberError) Error() string {
	if err.err == nil {
		return ""
	}
	name := proxyuri.FirstNonEmpty(err.member.id, err.member.tag)
	if name == "" {
		return err.err.Error()
	}
	return fmt.Sprintf("成员 %s 初始化失败: %v", name, err.err)
}

func (err dynamicMemberError) Unwrap() error {
	return err.err
}

func asDynamicMemberError(err error) (dynamicMemberError, bool) {
	var memberErr dynamicMemberError
	if errors.As(err, &memberErr) && (memberErr.member.id != "" || memberErr.member.tag != "") {
		return memberErr, true
	}
	return dynamicMemberError{}, false
}

func runtimeFailureFromMapping(mapping *tables.PortMappingTable, err error) RuntimeInboundFailure {
	return RuntimeInboundFailure{
		MappingID: mapping.ID,
		Tag:       mappingInboundTag(mapping.ID),
		Listen:    mappingRuntimeListen(mapping),
		Error:     err.Error(),
	}
}

func runtimeFailureFromInbound(inbound RuntimeInbound, err error) RuntimeInboundFailure {
	return RuntimeInboundFailure{
		MappingID: inbound.MappingID,
		Tag:       inbound.Tag,
		Listen:    inbound.Listen,
		Error:     err.Error(),
	}
}

func nodeFromOutboundInitializeError(
	err error,
	outbounds []option.Outbound,
	outboundNodes map[string]*tables.ProxyNodeTable,
) *tables.ProxyNodeTable {
	if err == nil || len(outbounds) == 0 || len(outboundNodes) == 0 {
		return nil
	}
	index, ok := outboundInitializeErrorIndex(err.Error())
	if !ok || index < 0 || index >= len(outbounds) {
		return nil
	}
	return outboundNodes[outbounds[index].Tag]
}

func nodeFromDynamicMember(plan *dynamicRuntimePlan, member dynamicMemberPlan) *tables.ProxyNodeTable {
	if plan == nil || len(plan.outboundNodes) == 0 {
		return nil
	}
	if node := plan.outboundNodes[member.tag]; node != nil {
		return node
	}
	for _, tag := range member.outboundTags() {
		if node := plan.outboundNodes[tag]; node != nil {
			return node
		}
	}
	return nil
}

func outboundInitializeErrorIndex(message string) (int, bool) {
	const prefix = "initialize outbound["
	start := strings.Index(message, prefix)
	if start < 0 {
		return 0, false
	}
	start += len(prefix)
	end := strings.IndexByte(message[start:], ']')
	if end < 0 {
		return 0, false
	}
	index, err := strconv.Atoi(message[start : start+end])
	if err != nil {
		return 0, false
	}
	return index, true
}

func outboundTagForNode(outboundNodes map[string]*tables.ProxyNodeTable, node *tables.ProxyNodeTable) string {
	if node == nil {
		return ""
	}
	preferred := nodeOutboundTag(node.ID)
	if outboundNodes[preferred] != nil {
		return preferred
	}
	for tag, candidate := range outboundNodes {
		if candidate != nil && candidate.ID == node.ID {
			return tag
		}
	}
	return preferred
}

func runtimeExcludedNodeFromNode(
	mapping *tables.PortMappingTable,
	node *tables.ProxyNodeTable,
	tag string,
	err error,
) RuntimeExcludedNode {
	excluded := RuntimeExcludedNode{
		Tag:   tag,
		Error: errorString(err),
	}
	if mapping != nil {
		excluded.MappingID = mapping.ID
	}
	if node != nil {
		excluded.NodeID = node.ID
		excluded.NodeName = proxyuri.FirstNonEmpty(node.Name, node.ID)
	}
	return excluded
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
