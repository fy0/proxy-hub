package proxy

import (
	"errors"

	"proxy-hub/service/proxyuri"
)

var (
	// URI/协议解析错误以 proxyuri 为唯一来源，跨包 errors.Is 直接可用。
	ErrUnsupportedURI        = proxyuri.ErrUnsupportedURI
	ErrUnsupportedProtocol   = proxyuri.ErrUnsupportedProtocol
	ErrInvalidPort           = proxyuri.ErrInvalidPort
	ErrUTLSRequired          = proxyuri.ErrUTLSRequired
	ErrNodeNotFound          = errors.New("proxy node not found")
	ErrMappingNotFound       = errors.New("port mapping not found")
	ErrInvalidAddress        = errors.New("invalid listen address")
	ErrNoAvailableNode       = errors.New("no available node")
	ErrInvalidMapping        = errors.New("invalid port mapping")
	ErrListenPortTaken       = errors.New("listen port already exists")
	ErrSubscriptionNotFound  = errors.New("proxy subscription not found")
	ErrGroupNotFound         = errors.New("proxy group not found")
	ErrInvalidSubscription   = errors.New("invalid proxy subscription")
	ErrInvalidGroup          = errors.New("invalid proxy group")
	ErrInvalidHealthDuration = errors.New("invalid proxy health duration")
	ErrInvalidChain          = errors.New("invalid proxy chain")
	ErrInvalidSettingsBackup = errors.New("invalid proxy settings backup")
	ErrInvalidProbeURL       = errors.New("invalid proxy test url")
	ErrInvalidMappingSwitch  = errors.New("invalid mapping switch target")
)
